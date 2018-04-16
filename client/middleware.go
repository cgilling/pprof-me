package client

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"runtime/pprof"
)

type ProfileType int

const (
	NoProfileType ProfileType = iota
	CPUProfileType
	HeapProfileType

	ProfilingHTTPHeaderName  = "X-PProfMe-Profile"
	CPUProfilingHeaderValue  = "cpu"
	HeapProfilingHeaderValue = "heap"
)

type ctxKey int

const requestProfilingKey ctxKey = 1

func WithRequestProfiling(ctx context.Context, typ ProfileType) context.Context {
	return context.WithValue(ctx, requestProfilingKey, typ)
}

func ProfilingHeaderMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get(ProfilingHTTPHeaderName) {
		case CPUProfilingHeaderValue:
			r = r.WithContext(WithRequestProfiling(r.Context(), CPUProfileType))
		case HeapProfilingHeaderValue:
			r = r.WithContext(WithRequestProfiling(r.Context(), HeapProfileType))
		}
		h.ServeHTTP(w, r)
	})
}

func NewProfileRequestMiddleware(c *Client) func(h http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			finishUp, _ := startProfiling(r.Context(), c)
			defer finishUp()
			h.ServeHTTP(w, r)
		})
	}
}

func emptyFinisher() error {
	return nil
}

// TODO: figure out how we can notify the ids of profiles being created so that they
//		 can be logged or something else if that is desired

func startProfiling(ctx context.Context, c *Client) (func() error, error) {
	typ, enabled := ctx.Value(requestProfilingKey).(ProfileType)
	if !enabled {
		return emptyFinisher, nil
	}
	file, err := ioutil.TempFile("", "pprof-me")
	if err != nil {
		return emptyFinisher, err
	}
	closeAndDeleteTmpFile := func() {
		file.Close()
		os.Remove(file.Name())
	}
	switch typ {
	case CPUProfileType:
		if err := pprof.StartCPUProfile(file); err != nil {
			closeAndDeleteTmpFile()
			return emptyFinisher, err
		}
		return func() error {
			defer closeAndDeleteTmpFile()
			pprof.StopCPUProfile()
			_, err := file.Seek(0, os.SEEK_SET)
			if err != nil {
				return err
			}
			_, err = c.SendProfile(ctx, "TODO: fill this in", file)
			return err
		}, nil
	case HeapProfileType:
		err = pprof.Lookup("heap").WriteTo(file, 0)
		if err != nil {
			closeAndDeleteTmpFile()
			return emptyFinisher, err
		}
		_, err = c.SendProfile(ctx, "TODO: fill this in", file)
		if err != nil {
			closeAndDeleteTmpFile()
			return emptyFinisher, err
		}
		return func() error {
			defer closeAndDeleteTmpFile()
			err = file.Truncate(0)
			if err != nil {
				return err
			}
			err = pprof.Lookup("heap").WriteTo(file, 0)
			if err != nil {
				return err
			}
			_, err = c.SendProfile(ctx, "TODO: fill this in", file)
			return err
		}, nil
	default:
		closeAndDeleteTmpFile()
		return emptyFinisher, fmt.Errorf("unknown profile type: %v", typ)
	}

}
