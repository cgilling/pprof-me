package e2e

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	_ "net/http/pprof"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/cgilling/pprof-me/app"
	"github.com/cgilling/pprof-me/client"
	"github.com/cgilling/pprof-me/msg"
	"github.com/dghubble/sling"
)

func TestCPUHeader(t *testing.T) {
	config := app.Config{}
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
