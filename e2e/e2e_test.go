package e2e

import (
	"context"
	"fmt"
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

func TestCPUHeaderWithSymbolizer(t *testing.T) {
	// TODO: figure out how we to make this a server assigned addr
	//       rather than hard-coded
	config := app.Config{ListenAddr: ":1234"}
	a := app.New(config)
	go a.ListenAndServe()
	defer a.Shutdown(context.Background())
	time.Sleep(time.Second)
	var endpoint http.Handler
	endpoint = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := 1
		b := 2
		fmt.Fprintf(w, "a + b = %v\n", a+b)
	})
	c, err := client.New("http://"+a.Server.Addr, nil)
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

	sbase := sling.New().Base("http://127.0.0.1:1234")

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
	req, err = sbase.New().Get("profiles/").Path(fmt.Sprintf("%s/web", id)).Request()
	if err != nil {
		t.Fatalf("failed to create profile web request: %v", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to send profile web request (%q): %v", req.URL, err)
	}
	if exp, got := 200, resp.StatusCode; exp != got {
		t.Fatalf("profiles web reqest status code not as expected (%q): exp: %d, got: %d", req.URL, exp, got)
	}
	_, err = goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
}
