package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/kelseyhightower/envconfig"
	"github.com/kennygrant/sanitize"
	"github.com/pborman/uuid"
)

type Config struct {
	ListenAddr string `envconfig:"LISTEN_ADDR"`
}

type App struct {
	config Config
	router *httprouter.Router
	server *http.Server

	profiles ProfileStore

	symbolizerURLsMu sync.Mutex
	symbolizerURLs   map[string]*url.URL

	instancesMu      sync.Mutex
	instances        map[string]*PProfInstance
	nextInstancePort int
}

type ProfileStore interface {
	StoreProfile(id string, profile []byte) error
	StoreSymbols(id string, symbols []byte) error
	StoreBinary(md5sum string, binary []byte) error
	StoreBinaryMD5(id, name, md5 string) error
	GetProfile(id string) (profile []byte, err error)
	GetBinary(id string) (name string, binary []byte, err error)
	GetSymbols(id string) (symbols []byte, err error)
	HasBinaryMD5(md5 string) bool
	HasSymbols(id string) bool
}

func NewApp(c Config) *App {
	router := httprouter.New()
	app := &App{
		router:           router,
		config:           c,
		instances:        make(map[string]*PProfInstance),
		nextInstancePort: 8888,
		profiles:         NewMemStore(),
		symbolizerURLs:   make(map[string]*url.URL),
		server: &http.Server{
			Addr:    c.ListenAddr,
			Handler: router,
		},
	}
	router.PUT("/binaries/:md5", app.BinaryPUT)
	router.POST("/profiles", app.ProfilePOST)
	router.GET("/profiles/:id", app.ProfileIDGET)
	router.GET("/profiles/:id/web/*subpath", app.ProfileProxy)
	router.PUT("/profiles/:id/web/*subpath", app.ProfileProxy)
	router.POST("/profiles/:id/web/*subpath", app.ProfileProxy)
	router.POST("/profiles/:id/debug/pprof/symbol", app.PProfSymbolPOST)
	router.GET("/profiles/:id/debug/pprof/profile", app.PProfProfileGET)

	return app
}

type ProfilePostRequest struct {
	Profile       []byte `json:"profile"`
	BinaryName    string `json:"binary_name"`
	BinaryMD5     string `json:"binary_md5"`
	SymoblizerURL string `json:"symbolizer_url"`
}

type ProfilePostResponse struct {
	ID                string `json:"id"`
	BinaryNeedsUpload bool   `json:"binary_needs_upload"`
}

func (app *App) PProfProfileGET(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	b, err := app.profiles.GetProfile(id)
	if err != nil {
		// TODO: differentiate between this and failure to read
		w.WriteHeader(404)
		fmt.Fprintf(w, "could not find profile for %q", id)
		return
	}
	w.Write(b)
}

func (app *App) PProfSymbolPOST(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	app.symbolizerURLsMu.Lock()
	symbolizerURL := app.symbolizerURLs[id]
	app.symbolizerURLsMu.Unlock()

	if symbolizerURL != nil {
		var symbols []byte
		rp := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				// slightly modified from https://golang.org/src/net/http/httputil/reverseproxy.go?s=2588:2649#L80
				req.URL.Scheme = symbolizerURL.Scheme
				req.URL.Host = symbolizerURL.Host
				req.URL.Path = symbolizerURL.Path
				if symbolizerURL.RawQuery == "" || req.URL.RawQuery == "" {
					req.URL.RawQuery = symbolizerURL.RawQuery + req.URL.RawQuery
				} else {
					req.URL.RawQuery = symbolizerURL.RawQuery + "&" + req.URL.RawQuery
				}
				if _, ok := req.Header["User-Agent"]; !ok {
					// explicitly disable User-Agent so it's not set to default value
					req.Header.Set("User-Agent", "")
				}
			},
			ModifyResponse: func(resp *http.Response) error {
				var err error
				symbols, err = ioutil.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					return err
				}
				resp.Body = ioutil.NopCloser(bytes.NewReader(symbols))
				return nil
			},
		}
		rp.ServeHTTP(w, r)
		if symbols == nil {
			w.WriteHeader(502)
			fmt.Fprintf(w, "failed to get symbols from: %q", symbolizerURL.String())
			return
		}
		app.symbolizerURLsMu.Lock()
		delete(app.symbolizerURLs, id)
		app.symbolizerURLsMu.Unlock()

		err := app.profiles.StoreSymbols(id, symbols)
		if err != nil {
			w.WriteHeader(500)
			fmt.Fprintf(w, "failed to store symbols: %v", err)
			return
		}
		return
	}

	if !app.profiles.HasSymbols(id) {
		w.WriteHeader(404)
		fmt.Fprintf(w, "could not find symbols for %q", id)
		return
	}

	b, err := app.profiles.GetSymbols(id)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "failed to get symbols: %v", err)
		return
	}
	w.Write(b)
}

func (app *App) ProfilePOST(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var req ProfilePostRequest
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&req)
	if err != nil {
		w.WriteHeader(400)
		fmt.Fprintf(w, "error parsing body: %v", err)
		return
	}
	if req.BinaryMD5 == "" && req.SymoblizerURL == "" {
		w.WriteHeader(400)
		fmt.Fprintf(w, `at least one of "binary_md5" or "symbolizer_url" required`)
		return
	}
	var resp ProfilePostResponse
	resp.ID = uuid.New()
	if req.BinaryMD5 != "" {
		err = app.profiles.StoreBinaryMD5(resp.ID, req.BinaryName, req.BinaryMD5)
		if err != nil {
			w.WriteHeader(500)
			fmt.Fprintf(w, "error storing binary info: %v", err)
			return
		}

		if !app.profiles.HasBinaryMD5(req.BinaryMD5) {
			resp.BinaryNeedsUpload = true
		}
	}

	err = app.profiles.StoreProfile(resp.ID, req.Profile)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "error storing profile: %v", err)
		return
	}

	if req.SymoblizerURL != "" {
		url, err := url.Parse(req.SymoblizerURL)
		if err != nil {
			w.WriteHeader(400)
			fmt.Fprintf(w, "error parsing symbolizer_url: %v", err)
			return
		}
		app.symbolizerURLsMu.Lock()
		app.symbolizerURLs[resp.ID] = url
		app.symbolizerURLsMu.Unlock()

		// TODO: call pprof to so we can get this symbol info

		cmd := exec.Command("pprof", "-top", fmt.Sprintf("http://%s/profiles/%s/debug/pprof/profile", app.config.ListenAddr, resp.ID))
		err = cmd.Run()

		if !app.profiles.HasSymbols(resp.ID) {
			w.WriteHeader(502)
			fmt.Fprintf(w, "failed to fetch symbols")
			return
		}
	}

	enc := json.NewEncoder(w)
	enc.Encode(resp)
}

func (app *App) ProfileIDGET(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
}

func (app *App) ProfileProxy(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var err error
	id := ps.ByName("id")
	app.instancesMu.Lock()
	inst, ok := app.instances[id]
	if !ok {
		inst, err = NewPProfInstance(context.TODO(), app.config.ListenAddr, fmt.Sprintf("/profiles/%s/web/", id), id, app.nextInstancePort, app.profiles)
		if err == nil {
			app.instances[id] = inst
			app.nextInstancePort++
		} else {
			inst = nil
		}
	}
	app.instancesMu.Unlock()
	if inst == nil {
		if err != nil {
			w.WriteHeader(500)
			fmt.Fprintf(w, `failed to create pprof proxy: %v`, err)
			return
		}
		// TODO: handle error
		return
	}
	for i := 0; i < 10; i++ {
		if inst.CheckIsActive() {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !inst.CheckIsActive() {
		w.WriteHeader(500)
		fmt.Fprintf(w, `pprof failed to start listening on port in reasonable amout of time`)
		return
	}
	inst.handler.ServeHTTP(w, r)
}

func (app *App) BinaryPUT(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var err error
	md5in := ps.ByName("md5")
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(400)
		fmt.Fprintf(w, `failed to read request body: %v`, err)
		return
	}
	h := md5.New()
	h.Write(b)
	md5sum := hex.EncodeToString(h.Sum(nil))
	if md5sum != md5in {
		w.WriteHeader(400)
		fmt.Fprintf(w, `md5sum in path (%s) does not match md5 of put contents (%s)`, md5in, md5sum)
		return
	}
	err = app.profiles.StoreBinary(md5sum, b)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, `failed to store binary: %v`, err)
		return
	}
}

type PProfInstance struct {
	handler  http.Handler
	runner   *PProfRunner
	isActive int64
}

func NewPProfInstance(ctx context.Context, listenAddr, pathPrefix, id string, port int, profiles ProfileStore) (*PProfInstance, error) {
	runner, err := NewPProfRunner(ctx, listenAddr, port, id, profiles)
	if err != nil {
		return nil, err
	}
	url, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	rp := httputil.NewSingleHostReverseProxy(url)
	rp.ModifyResponse = func(resp *http.Response) error {
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()
		newb := bytes.Replace(b, []byte(`href="/`), []byte(fmt.Sprintf(`href="%s`, pathPrefix)), -1)
		newb = bytes.Replace(newb, []byte(`new URL("/`), []byte(fmt.Sprintf(`new URL("%s`, pathPrefix)), -1)
		resp.Body = ioutil.NopCloser(bytes.NewReader(newb))
		return nil
	}
	go runner.Run()
	return &PProfInstance{
		handler: http.StripPrefix(pathPrefix, rp),
		runner:  runner,
	}, nil
}

func (ppi *PProfInstance) CheckIsActive() bool {
	if atomic.LoadInt64(&ppi.isActive) > 0 {
		return true
	}
	_, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ppi.runner.Port), 10*time.Millisecond)
	if err != nil {
		return false
	}
	atomic.StoreInt64(&ppi.isActive, 1)
	return true
}

type PProfRunner struct {
	Port     int
	Profiles ProfileStore

	StartTime  time.Time
	Cmd        *exec.Cmd
	TmpDirPath string
}

func NewPProfRunner(ctx context.Context, serverListenAddr string, port int, id string, profiles ProfileStore) (*PProfRunner, error) {
	var cmd *exec.Cmd
	var tmpDirPath string
	if profiles.HasSymbols(id) {

		cmd = exec.CommandContext(ctx, "pprof", fmt.Sprintf("-http=127.0.0.1:%d", port), fmt.Sprintf("http://%s/profiles/%s/debug/pprof/profile", serverListenAddr, id))
	} else {
		name, binary, err := profiles.GetBinary(id)
		if err != nil {
			return nil, err
		}
		profile, err := profiles.GetProfile(id)
		if err != nil {
			return nil, err
		}
		tmpDirPath, err := ioutil.TempDir("", fmt.Sprintf("pprof-me-%s", id))
		if err != nil {
			return nil, err
		}
		binPath := filepath.Join(tmpDirPath, sanitize.Name(name))
		err = ioutil.WriteFile(binPath, binary, 0700)
		if err != nil {
			return nil, err
		}
		profilePath := filepath.Join(tmpDirPath, "profile")
		err = ioutil.WriteFile(profilePath, profile, 0700)
		if err != nil {
			return nil, err
		}
		cmd = exec.CommandContext(ctx, "pprof", fmt.Sprintf("-http=127.0.0.1:%d", port), binPath, profilePath)
	}
	pr := &PProfRunner{
		Port:       port,
		Profiles:   profiles,
		Cmd:        cmd,
		TmpDirPath: tmpDirPath,
	}
	return pr, nil
}

func (pr *PProfRunner) Run() error {
	return pr.Cmd.Run()
}

func (pr *PProfRunner) Close() error {
	pr.Cmd.Process.Kill()
	if pr.TmpDirPath != "" {
		return os.RemoveAll(pr.TmpDirPath)
	}
	return nil
}

func (app *App) ListenAndServe() error {
	return app.server.ListenAndServe()
}

func (app *App) Shutdown(ctx context.Context) error {
	err := app.server.Shutdown(ctx)
	for _, inst := range app.instances {
		inst.runner.Close()
	}
	return err
}

func main() {
	var config Config
	err := envconfig.Process("PPROF_ME", &config)
	if err != nil {
		log.Fatal(err.Error())
	}
	app := NewApp(config)

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)

	go app.ListenAndServe()

	<-shutdown
	fmt.Println("shutting down")

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	app.Shutdown(ctx)

}
