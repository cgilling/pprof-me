package client

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/cgilling/pprof-me/msg"
	"github.com/dghubble/sling"
)

type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	base      *sling.Sling
	urlPrefix string
	binaryMD5 string
}

func New(urlPrefix string, d Doer) (*Client, error) {
	if d == nil {
		d = http.DefaultClient
	}
	if urlPrefix[len(urlPrefix)-1] != '/' {
		urlPrefix += "/"
	}
	fp, err := os.Open(os.Args[0])
	if err != nil {
		return nil, fmt.Errorf("failed to open binary to create md5 hash: %v", err)
	}
	defer fp.Close()
	h := md5.New()
	_, err = io.Copy(h, fp)
	if err != nil {
		return nil, fmt.Errorf("failed to create md5 of binary: %v", err)
	}
	return &Client{
		base:      sling.New().Base(urlPrefix).Doer(d),
		urlPrefix: urlPrefix,
		binaryMD5: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func (c *Client) SendProfile(ctx context.Context, name string, r io.Reader) (string, error) {
	profile, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}
	if name == "" {
		name = filepath.Base(os.Args[0])
	}
	preq := msg.ProfilePostRequest{
		Profile:   profile,
		AppName:   name,
		BinaryMD5: c.binaryMD5,
	}

	var presp msg.ProfilePostResponse
	resp, err := c.base.New().Post("profiles").BodyJSON(preq).ReceiveSuccess(&presp)
	if err != nil {
		return "", err
	}
	if exp, got := 201, resp.StatusCode; exp != got {
		return "", fmt.Errorf("POST /profiles returned unexpected status code: exp: %d, got: %d", exp, got)
	}
	return presp.ID, nil
}

func getLocalAddr(ctx context.Context, targetURL string) (string, error) {
	d := &net.Dialer{}
	u, err := url.Parse(targetURL)
	if err != nil {
		return "", err
	}
	conn, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return conn.LocalAddr().String(), nil
}
