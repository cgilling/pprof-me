package app

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"sync"
	"time"

	"github.com/cgilling/pprof-me/msg"
	"github.com/julienschmidt/httprouter"
	"github.com/pborman/uuid"
)

type Config struct {
	ListenAddr string `envconfig:"LISTEN_ADDR"`
}

type App struct {
	listener net.Listener
	Server   *http.Server

	config Config
	router *httprouter.Router

	profiles ProfileStore

	symbolizerURLsMu sync.Mutex
	symbolizerURLs   map[string]*url.URL

	instancesMu      sync.Mutex
	instances        map[string]*PProfInstance
	nextInstancePort int
}

func New(c Config) (*App, error) {
	ln, err := net.Listen("tcp", c.ListenAddr)
	if err != nil {
		return nil, err
	}
	c.ListenAddr = ln.Addr().String()
	router := httprouter.New()
	app := &App{
		router:           router,
		config:           c,
		instances:        make(map[string]*PProfInstance),
		nextInstancePort: 8888,
		profiles:         NewMemStore(),
		symbolizerURLs:   make(map[string]*url.URL),
		listener:         ln,
		Server: &http.Server{
			Handler: router,
		},
	}
	router.PUT("/binaries/:md5", app.BinaryPUT)
	router.POST("/profiles", app.ProfilePOST)
	router.GET("/profiles", app.ProfileList)
	router.GET("/profiles/:id", app.PProfProfileGET)
	router.GET("/profiles/:id/web/*subpath", app.ProfileProxy)
	router.PUT("/profiles/:id/web/*subpath", app.ProfileProxy)
	router.POST("/profiles/:id/web/*subpath", app.ProfileProxy)
	router.POST("/profiles/:id/debug/pprof/symbol", app.PProfSymbolPOST)
	router.GET("/profiles/:id/debug/pprof/profile", app.PProfProfileGET)

	return app, nil
}

func (app *App) ListenAndServe() error {
	return app.Server.Serve(app.listener)
}

func (app *App) Addr() string {
	return app.config.ListenAddr
}

func (app *App) Shutdown(ctx context.Context) error {
	err := app.Server.Shutdown(ctx)
	for _, inst := range app.instances {
		inst.runner.Close()
	}
	return err
}

func (app *App) ProfileList(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var err error
	var resp msg.ProfileListResponse
	resp.Profiles, err = app.profiles.ListProfiles()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "failed to ListProfiles: %v", err)
		return
	}

	w.WriteHeader(200)
	enc := json.NewEncoder(w)
	enc.Encode(resp)
}

func (app *App) ProfilePOST(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var req msg.ProfilePostRequest
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
	var resp msg.ProfilePostResponse
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

		// NOTE: we make this call (-top) because it will exit without any human interaction, therefore we
		//       know that when this command exits, the appropriate calls to get the symbols will have
		//       been called and we will have saved the symbols to the profile store.
		cmd := exec.Command("pprof", "-top", fmt.Sprintf("http://%s/profiles/%s/debug/pprof/profile", app.config.ListenAddr, resp.ID))
		err = cmd.Run()

		if !app.profiles.HasSymbols(resp.ID) {
			w.WriteHeader(502)
			fmt.Fprintf(w, "failed to fetch symbols")
			return
		}
	}
	w.WriteHeader(201)
	enc := json.NewEncoder(w)
	enc.Encode(resp)
}

func (app *App) ProfileProxy(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var err error
	id := ps.ByName("id")
	app.instancesMu.Lock()
	inst, ok := app.instances[id]
	if !ok {
		inst, err =
			NewPProfInstance(
				fmt.Sprintf("http://%s/profiles/%s/debug/pprof/profile", app.config.ListenAddr, id),
				fmt.Sprintf("/profiles/%s/web/", id),
				id, app.nextInstancePort, app.profiles)
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
	enc := json.NewEncoder(w)
	enc.Encode(map[string]string{"msg": "success"})
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
