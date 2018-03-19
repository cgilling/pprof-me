package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/kennygrant/sanitize"
)

type PProfInstance struct {
	handler  http.Handler
	runner   *PProfRunner
	isActive int64
}

func NewPProfInstance(ctx context.Context, pprofProfileURL, pathPrefix, id string, port int, profiles ProfileStore) (*PProfInstance, error) {
	runner, err := NewPProfRunner(ctx, pprofProfileURL, port, id, profiles)
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

func NewPProfRunner(ctx context.Context, pprofProfileURL string, port int, id string, profiles ProfileStore) (*PProfRunner, error) {
	tmpDirPath, err := ioutil.TempDir("", fmt.Sprintf("pprof-me-%s", id))
	if err != nil {
		return nil, err
	}
	cmd, err := func() (*exec.Cmd, error) {
		if profiles.HasSymbols(id) {
			return exec.CommandContext(ctx, "pprof", "-symbolize=remote", fmt.Sprintf("-http=127.0.0.1:%d", port), pprofProfileURL), nil
		} else {
			name, binary, err := profiles.GetBinary(id)
			if err != nil {
				return nil, err
			}
			profile, err := profiles.GetProfile(id)
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
			return exec.CommandContext(ctx, "pprof", fmt.Sprintf("-http=127.0.0.1:%d", port), binPath, profilePath), nil
		}
	}()
	if err != nil {
		os.RemoveAll(tmpDirPath)
	}
	cmd.Env = append(os.Environ(), fmt.Sprintf("PPROF_TMPDIR=%s", tmpDirPath))
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
