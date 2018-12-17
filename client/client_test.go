package client

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgilling/pprof-me/msg"
	"github.com/dankinder/httpmock"
	"github.com/stretchr/testify/mock"
)

var binaryMD5 string

func init() {
	// NOTE: I normally dislike implementing logic in tests that directly
	//		 replicates how the code itself calculates the same value, but
	//		 in this instance there doesn't seem to be a better way of doing
	//       this as the md5 of the test binary will be changed with every
	//       change to the test (or more), so this value can't be merely
	//       hardcoded.
	fp, err := os.Open(os.Args[0])
	if err != nil {
		panic(fmt.Errorf("failed to open binary to create md5: %v", err))
	}
	defer fp.Close()
	h := md5.New()
	_, err = io.Copy(h, fp)
	if err != nil {
		panic(fmt.Errorf("failed to create md5 of binary: %v", err))
	}
	binaryMD5 = hex.EncodeToString(h.Sum(nil))

	fmt.Printf("binary MD5: %v\n", binaryMD5)
}

func TestClientUploadProfile(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	ppme := &httpmock.MockHandler{}

	s := httpmock.NewServer(ppme)
	defer s.Close()

	c, err := New(s.URL(), nil)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	msgResp := msg.ProfilePostResponse{
		ID: "23E3490F-9F1F-4A19-9EBB-07592A7A1ED0",
	}
	msgReq := msg.ProfilePostRequest{
		Profile:   []byte("hellothere"),
		AppName:   filepath.Base(os.Args[0]),
		BinaryMD5: binaryMD5,
	}

	ppme.On("Handle", "POST", "/profiles", httpmock.JSONMatcher(&msgReq)).Return(httpmock.Response{
		Status: 201,
		Body:   httpmock.ToJSON(msgResp),
	})

	id, err := c.SendProfile(context.Background(), "", strings.NewReader("hellothere"))
	if err != nil {
		t.Fatalf("failed to SendProfile: %v", err)
	}
	if exp, got := msgResp.ID, id; exp != got {
		t.Errorf("returned ID not as expected: exp: %v, got: %v", exp, got)
	}

	ppme.AssertExpectations(t)
}

func TestSendProfileReturnsErrorOnNon201Response(t *testing.T) {
	ppme := &httpmock.MockHandler{}

	s := httpmock.NewServer(ppme)
	defer s.Close()

	c, err := New(s.URL(), nil)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ppme.On("Handle", "POST", "/profiles", mock.Anything).Return(httpmock.Response{
		Status: 500,
	})

	_, err = c.SendProfile(context.Background(), "", strings.NewReader("hellothere"))
	if err == nil {
		t.Errorf("expected SendProfile to return error but it didn't")
	}

	ppme.AssertExpectations(t)
}
