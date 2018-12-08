package app

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"sync/atomic"
	"time"
)

type PProfInstance struct {
	handler  http.Handler
	runner   *PProfRunner
	isActive int64
}

func NewPProfInstance(pprofProfileURL, pathPrefix, id string, port int) (*PProfInstance, error) {
	runner, err := NewPProfRunner(pprofProfileURL, port, id)
	if err != nil {
		return nil, err
	}
	url, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d/ui", port))
	if err != nil {
		return nil, err
	}
	rp := httputil.NewSingleHostReverseProxy(url)
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
	Port int

	StartTime  time.Time
	Cmd        *exec.Cmd
	TmpDirPath string
}

func NewPProfRunner(pprofProfileURL string, port int, id string) (*PProfRunner, error) {
	tmpDirPath, err := ioutil.TempDir("", fmt.Sprintf("pprof-me-%s", id))
	if err != nil {
		return nil, err
	}
	cmd, err := exec.Command("pprof", fmt.Sprintf("-http=127.0.0.1:%d", port), pprofProfileURL), nil
	if err != nil {
		os.RemoveAll(tmpDirPath)
		return nil, err
	}
	cmd.Env = append(os.Environ(), fmt.Sprintf("PPROF_TMPDIR=%s", tmpDirPath))
	pr := &PProfRunner{
		Port:       port,
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
