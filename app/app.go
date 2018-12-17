package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/cgilling/pprof-me/kube"
	"github.com/cgilling/pprof-me/msg"
	"github.com/cgilling/pprof-me/reqproxy"
	"github.com/julienschmidt/httprouter"
	"github.com/pborman/uuid"
)

type Config struct {
	ListenAddr string      `envconfig:"LISTEN_ADDR"`
	Kube       kube.Config `envconfig:"KUBE"`
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

	podProvider *kube.PodProvider

	proxiesMu      sync.Mutex
	profileProxies map[string]reqproxy.RequestProxy
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
		profileProxies:   make(map[string]reqproxy.RequestProxy),
		listener:         ln,
		Server: &http.Server{
			Handler: router,
		},
	}
	if c.Kube.InCluster || c.Kube.ConfigPath != "" {
		app.podProvider, err = kube.NewPodProvider(c.Kube)
		if err != nil {
			return nil, err
		}
		router.GET("/kube/pods", app.KubePodsGET)
	}

	router.POST("/profiles", app.ProfilePOST)
	router.GET("/profiles", app.ProfileList)
	router.GET("/profiles/:id", app.PProfProfileGET)
	router.GET("/profiles/:id/ui/*subpath", app.ProfileProxy)
	router.PUT("/profiles/:id/ui/*subpath", app.ProfileProxy)
	router.POST("/profiles/:id/ui/*subpath", app.ProfileProxy)
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

func (app *App) KubePodsGET(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pods, err := app.podProvider.GetPods()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "failed to GetPods: %v", err)
		return
	}
	enc := json.NewEncoder(w)
	enc.Encode(pods)
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

	if req.Kube != nil {
		app.handleKubeProfileProxy(w, &req)
		return
	}
	var resp msg.ProfilePostResponse
	resp.ID = app.profiles.CreateID(req.AppName)
	meta := ProfileMetadata{BinaryMD5: req.BinaryMD5}
	err = app.profiles.StoreProfile(resp.ID, req.Profile, meta)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "error storing profile: %v", err)
		return
	}

	w.WriteHeader(201)
	enc := json.NewEncoder(w)
	enc.Encode(resp)
}

func (app *App) handleKubeProfileProxy(w http.ResponseWriter, req *msg.ProfilePostRequest) {
	pods, err := app.podProvider.GetPods()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "getting pods: %v", err)
		return
	}
	var pod kube.Pod
	for _, p := range pods {
		if p.ObjectMeta.Namespace == req.Kube.Namespace &&
			p.ObjectMeta.Name == req.Kube.PodName {
			pod = p
			break
		}
	}
	if pod.Pod == nil {
		w.WriteHeader(404)
		fmt.Fprintf(w, "failed to find requested pod: %v", req.Kube)
		return
	}

	path := "/debug/pprof/profile"
	if req.Kube.ProfileType == "HEAP" {
		path = "/debug/pprof/heap"
	}
	proxy := app.podProvider.NewProxy(pod, path)

	var resp msg.ProfilePostResponse
	resp.ID = uuid.New()
	app.proxiesMu.Lock()
	app.profileProxies[resp.ID] = proxy
	app.proxiesMu.Unlock()
	cmd := exec.Command("pprof", "-top", fmt.Sprintf("http://%s/profiles/%s/debug/pprof/profile", app.config.ListenAddr, resp.ID))
	err = cmd.Run()

	// just in case something went wrong and the proxy was cleaned up after use, we want to
	// make sure it doesn't stay around forever.
	app.proxiesMu.Lock()
	app.profileProxies[resp.ID] = nil
	app.proxiesMu.Unlock()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "failed to fetch profile: %v", err)
		return
	}

	// TODO: probably want to add in a `HasProfile` call, also we could consider having the
	//		 request that uses the profileProxy actually be able to write success/failure in
	//       the same hash rather than having to do another request here to the profile store.
	if _, _, err = app.profiles.GetProfile(resp.ID); err != nil {
		w.WriteHeader(502)
		fmt.Fprintf(w, "failed to fetch profile")
		return
	}

	// TODO: refactor
	w.WriteHeader(201)
	enc := json.NewEncoder(w)
	enc.Encode(resp)
}

func (app *App) PProfProfileGET(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	app.proxiesMu.Lock()
	profileProxy := app.profileProxies[id]
	app.proxiesMu.Unlock()

	if profileProxy != nil {
		profile, err := profileProxy.ProxyAndReturnBody(w, r)
		if err != nil || profile == nil {
			fmt.Printf("failed to get profile from: %q %v\n", profileProxy, err)
			return
		}
		app.proxiesMu.Lock()
		delete(app.profileProxies, id)
		app.proxiesMu.Unlock()

		err = app.profiles.StoreProfile(id, profile, ProfileMetadata{})
		if err != nil {
			fmt.Printf("failed to store profile: %v\n", err)
			return
		}
		return
	}

	b, _, err := app.profiles.GetProfile(id)
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
				fmt.Sprintf("/profiles/%s/ui/", id),
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
