package e2e

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	_ "net/http/pprof"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/cgilling/pprof-me/app"
	"github.com/cgilling/pprof-me/client"
	"github.com/cgilling/pprof-me/msg"
	"github.com/cgilling/pprof-me/store"
	"github.com/dghubble/sling"
)

const (
	s3BucketName = "pprof-me-test"
)

var (
	useAWS     = false
	s3Endpoint string
)

func init() {
	s3Endpoint = os.Getenv("TEST_S3_ENDPOINT")
	if s3Endpoint == "" {
		return
	}
	url, err := url.Parse(s3Endpoint)
	if err != nil {
		panic("TEST_S3_ENDPOINT not a valid URL")
	}
	// NOTE: as we use localstack for testing, it can take some time to start up, so we need
	//		 to make sure our tests will wait for it to start up before proceeding.
	for i := 0; i < 20; i++ {
		_, err := net.DialTimeout("tcp", url.Hostname()+":"+url.Port(), 5*time.Second)
		if err == nil {
			break
		}
		log.Println(err)
		time.Sleep(time.Second)
	}
	s3cli := s3.New(session.Must(session.NewSession()), &aws.Config{
		Endpoint:         aws.String(s3Endpoint),
		S3ForcePathStyle: aws.Bool(true),
	})
	_, err = s3cli.CreateBucket(&s3.CreateBucketInput{Bucket: aws.String(s3BucketName)})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeBucketAlreadyExists:
				fallthrough
			case s3.ErrCodeBucketAlreadyOwnedByYou:
			default:
				panic(err)
			}
		} else {
			panic(err)
		}
	}
	useAWS = true
}

func clearTestBucket(t *testing.T) {
	s3cli := s3.New(session.Must(session.NewSession()), &aws.Config{
		Endpoint:         aws.String(s3Endpoint),
		S3ForcePathStyle: aws.Bool(true),
	})
	err := s3cli.ListObjectsPages(&s3.ListObjectsInput{Bucket: aws.String(s3BucketName)},
		func(page *s3.ListObjectsOutput, lastPage bool) bool {
			for _, obj := range page.Contents {
				_, err := s3cli.DeleteObject(&s3.DeleteObjectInput{
					Bucket: aws.String(s3BucketName),
					Key:    obj.Key,
				})
				if err != nil {
					t.Fatalf("failed to delete existing file: %v", err)
				}
			}
			return true
		})
	if err != nil {
		t.Fatalf("failed to list objects in test bucket: %v", err)
	}
}

func TestCPUHeader(t *testing.T) {
	config := app.Config{}
	if useAWS {
		config.AWS = store.AWSConfig{
			BucketName: s3BucketName,
			S3Endpoint: s3Endpoint,
		}
		clearTestBucket(t)
	}
	a, err := app.New(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	go a.ListenAndServe()
	defer a.Shutdown(context.Background())
	time.Sleep(time.Second)
	var endpoint http.Handler
	endpoint = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := 1
		b := 2
		fmt.Fprintf(w, "a + b = %v\n", a+b)
	})
	c, err := client.New("http://"+a.Addr(), nil)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	endpoint = client.NewProfileRequestMiddleware(c)(endpoint)
	endpoint = client.ProfilingHeaderMiddleware(endpoint)
	mux := http.NewServeMux()
	mux.Handle("/debug/pprof", http.HandlerFunc(http.DefaultServeMux.ServeHTTP))
	mux.Handle("/", endpoint)

	s := httptest.NewServer(mux)
	defer s.Close()

	req, _ := http.NewRequest("GET", s.URL, nil)
	req.Header.Set(client.ProfilingHTTPHeaderName, client.CPUProfilingHeaderValue)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to do request: %v", err)
	}
	if exp, got := 200, resp.StatusCode; exp != got {
		t.Fatalf("return code from test endpoint not as expected: exp: %d, got: %d", exp, got)
	}

	sbase := sling.New().Base("http://" + a.Addr())

	var lresp msg.ProfileListResponse
	resp, err = sbase.New().Get("profiles").ReceiveSuccess(&lresp)
	if err != nil {
		t.Fatalf("received error making list response: %v", err)
	}
	if exp, got := 200, resp.StatusCode; exp != got {
		t.Fatalf("list profiles response code not as expected: exp: %d, got: %d", exp, got)
	}
	if exp, got := 1, len(lresp.Profiles); exp != got {
		t.Fatalf("profile count not as expected: exp: %d, got: %d", exp, got)
	}

	id := lresp.Profiles[0].ID
	req, err = sbase.New().Get("profiles/").Path(fmt.Sprintf("%s/ui", id)).Request()
	if err != nil {
		t.Fatalf("failed to create profile ui request: %v", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to send profile ui request (%q): %v", req.URL, err)
	}
	defer resp.Body.Close()
	if exp, got := 200, resp.StatusCode; exp != got {
		b, _ := ioutil.ReadAll(resp.Body)
		t.Fatalf("profiles ui reqest status code not as expected (%q): exp: %d, got: %d\nresp body: %v", req.URL, exp, got, string(b))
	}
	_, err = goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
}
