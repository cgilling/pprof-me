package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
		listener:         ln,
		Server: &http.Server{
			Handler: router,
		},
	}

	router.POST("/profiles", app.ProfilePOST)
	router.GET("/profiles", app.ProfileList)
	router.GET("/profiles/:id", app.PProfProfileGET)
	router.GET("/profiles/:id/web/*subpath", app.ProfileProxy)
	router.PUT("/profiles/:id/web/*subpath", app.ProfileProxy)
	router.POST("/profiles/:id/web/*subpath", app.ProfileProxy)
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

	var resp msg.ProfilePostResponse
	resp.ID = uuid.New()
	err = app.profiles.StoreProfile(resp.ID, req.Profile)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "error storing profile: %v", err)
		return
	}

	w.WriteHeader(201)
	enc := json.NewEncoder(w)
	enc.Encode(resp)
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
				id, app.nextInstancePort)
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
