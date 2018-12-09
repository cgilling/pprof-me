package reqproxy

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
)

type RequestProxy interface {
	String() string
	ProxyAndReturnBody(w http.ResponseWriter, r *http.Request) ([]byte, error)
}

type URLProxy struct {
	target *url.URL
	rp     *httputil.ReverseProxy
}

func NewURLProxy(target *url.URL) *URLProxy {
	return &URLProxy{target: target}
}

func (p *URLProxy) String() string {
	return p.target.String()
}

func (p *URLProxy) ProxyAndReturnBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	var body []byte
	var err error
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// slightly modified from https://golang.org/src/net/http/httputil/reverseproxy.go?s=2588:2649#L80
			req.URL.Scheme = p.target.Scheme
			req.URL.Host = p.target.Host
			req.URL.Path = p.target.Path
			if p.target.RawQuery == "" || req.URL.RawQuery == "" {
				req.URL.RawQuery = p.target.RawQuery + req.URL.RawQuery
			} else {
				req.URL.RawQuery = p.target.RawQuery + "&" + req.URL.RawQuery
			}
			if _, ok := req.Header["User-Agent"]; !ok {
				// explicitly disable User-Agent so it's not set to default value
				req.Header.Set("User-Agent", "")
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			body, err = ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
			resp.Body = ioutil.NopCloser(bytes.NewReader(body))
			return nil
		},
	}
	proxy.ServeHTTP(w, r)
	return body, err
}
