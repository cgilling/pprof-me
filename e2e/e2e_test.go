package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	_ "net/http/pprof"
	"testing"
	"time"

	"github.com/cgilling/pprof-me/app"
	"github.com/cgilling/pprof-me/client"
)

func TestCPUHeaderWithSymbolizer(t *testing.T) {
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
	_, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to do request: %v", err)
	}

}
