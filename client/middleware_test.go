package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cgilling/pprof-me/msg"
	"github.com/dankinder/httpmock"
	"github.com/stretchr/testify/mock"
)

func TestProfilingHeaderMiddleware(t *testing.T) {
	var ptype ProfileType
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ptype, _ = r.Context().Value(requestProfilingKey).(ProfileType)
	})
	h := ProfilingHeaderMiddleware(baseHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/endpoint", nil)
	req.Header.Set(ProfilingHTTPHeaderName, CPUProfilingHeaderValue)
	h.ServeHTTP(w, req)

	if exp, got := CPUProfileType, ptype; exp != got {
		t.Errorf("profile type not as expected: exp: %v, got: %v", exp, got)
	}

	req.Header.Set(ProfilingHTTPHeaderName, HeapProfilingHeaderValue)
	h.ServeHTTP(w, req)

	if exp, got := HeapProfileType, ptype; exp != got {
		t.Errorf("profile type not as expected: exp: %v, got: %v", exp, got)
	}

	req.Header.Del(ProfilingHTTPHeaderName)
	h.ServeHTTP(w, req)

	if exp, got := NoProfileType, ptype; exp != got {
		t.Errorf("profile type not as expected: exp: %v, got: %v", exp, got)
	}
}

func TestCPUProfile(t *testing.T) {
	ppme := &httpmock.MockHandler{}

	s := httpmock.NewServer(ppme)
	defer s.Close()

	c, err := New(s.URL(), nil)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	msgResp := msg.ProfilePostResponse{
		ID: "23E3490F-9F1F-4A19-9EBB-07592A7A1ED0",
	}

	// TODO: we need to figure out how to test that this is a CPU profile rather than
	//		 another kind of profile.
	ppme.On("Handle", "POST", "/profiles", mock.Anything).Return(httpmock.Response{
		Status: 201,
		Body:   httpmock.ToJSON(msgResp),
	})

	var h http.Handler
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mw := NewProfileRequestMiddleware(c)
	h = mw(h)
	req := httptest.NewRequest("GET", "http://does.not.exist/my/endpoint", nil)
	req = req.WithContext(WithRequestProfiling(req.Context(), CPUProfileType))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	ppme.AssertExpectations(t)
}

func TestHeapProfile(t *testing.T) {
	ppme := &httpmock.MockHandler{}

	s := httpmock.NewServer(ppme)
	defer s.Close()

	c, err := New(s.URL(), nil)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	msgResp := msg.ProfilePostResponse{
		ID: "23E3490F-9F1F-4A19-9EBB-07592A7A1ED0",
	}
	msgResp2 := msg.ProfilePostResponse{
		ID: "02A89E6F-6652-4B0A-B6E4-1383069E9CFA",
	}

	// TODO: we need to figure out how to test that this is a MEM profile rather than
	//		 another kind of profile.
	ppme.On("Handle", "POST", "/profiles", mock.Anything).Return(httpmock.Response{
		Status: 201,
		Body:   httpmock.ToJSON(msgResp),
	}).Once()
	ppme.On("Handle", "POST", "/profiles", mock.Anything).Return(httpmock.Response{
		Status: 201,
		Body:   httpmock.ToJSON(msgResp2),
	}).Once()

	var h http.Handler
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mw := NewProfileRequestMiddleware(c)
	h = mw(h)
	req := httptest.NewRequest("GET", "http://does.not.exist/my/endpoint", nil)
	req = req.WithContext(WithRequestProfiling(req.Context(), HeapProfileType))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	ppme.AssertExpectations(t)
}

func TestDoesntProfileWhenNotRequested(t *testing.T) {
	ppme := &httpmock.MockHandler{}
	ppme.On("Handle", "POST", "/profiles", mock.Anything).Return(httpmock.Response{
		Status: 200,
	})

	s := httpmock.NewServer(ppme)
	defer s.Close()

	c, err := New(s.URL(), nil)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	var h http.Handler
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mw := NewProfileRequestMiddleware(c)
	h = mw(h)
	req := httptest.NewRequest("GET", "http://does.not.exist/my/endpoint", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	ppme.AssertNotCalled(t, "Handle", "POST", "/profiles", mock.Anything)
}
