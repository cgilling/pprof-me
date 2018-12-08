package app

import (
	_ "net/http/pprof"
)

/* TODO: redo this test, we not longer want to support storing the binary, because profiles
		 now contain the symbol information in them.
func TestPProfInstanceWithBinary(t *testing.T) {
	id := "123456"

	var req *http.Request
	var w *httptest.ResponseRecorder
	var resp *http.Response

	req = httptest.NewRequest("GET", "http://does.not.exist/debug/pprof/heap", nil)
	w = httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, req)
	profile, _ := ioutil.ReadAll(w.Result().Body)

	binary, err := ioutil.ReadFile(os.Args[0])
	if err != nil {
		t.Fatalf("failed to read in binary: %v", err)
	}
	h := md5.New()
	h.Write(binary)
	md5sum := hex.EncodeToString(h.Sum(nil))

	m := NewMemStore()
	m.StoreProfile(id, profile)
	m.StoreBinary(md5sum, binary)
	m.StoreBinaryMD5(id, filepath.Base(os.Args[0]), md5sum)

	pi, err := NewPProfInstance("http://does.not.exist/profiles/123456/debug/pprof/profile", "/myprefix/", id, 8888, m)
	if err != nil {
		t.Errorf("NewPProfInstance returned error: %v", err)
	}
	defer pi.runner.Close()

	i := 0
	for ; i < 15; i++ {
		if pi.CheckIsActive() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !pi.CheckIsActive() {
		t.Errorf("failed to launch pprof successfully")
	}

	req = httptest.NewRequest("GET", "http://does.not.exist/myprefix/flamegraph", nil)
	w = httptest.NewRecorder()
	pi.handler.ServeHTTP(w, req)
	resp = w.Result()
	if exp, got := 200, resp.StatusCode; exp != got {
		t.Fatalf("status code not as expected: exp: %d, got: %d", exp, got)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		if strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "/myprefix/") {
			t.Errorf("expected link to start with /myprefix/: %q", href)
		}
	})

	req = httptest.NewRequest("GET", "http://does.not.exist/profiles/123456/web/nosuchpage", nil)
	w = httptest.NewRecorder()
	pi.handler.ServeHTTP(w, req)
	resp = w.Result()
	if exp, got := 404, resp.StatusCode; exp != got {
		t.Fatalf("status code not as expected for non-existent page: exp: %d, got: %d", exp, got)
	}
}
*/
