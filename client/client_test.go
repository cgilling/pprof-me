package client

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
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

func TestClientWithSymbols(t *testing.T) {
	ppme := &httpmock.MockHandler{}

	s := httpmock.NewServer(ppme)
	defer s.Close()

	c, err := New(s.URL(), nil)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	c.SymbolURL = "http://me/custom/symbols"

	msgResp := msg.ProfilePostResponse{
		ID:                "23E3490F-9F1F-4A19-9EBB-07592A7A1ED0",
		BinaryNeedsUpload: false,
	}
	msgReq := msg.ProfilePostRequest{
		Profile:       []byte("hellothere"),
		BinaryName:    filepath.Base(os.Args[0]),
		BinaryMD5:     binaryMD5,
		SymoblizerURL: c.SymbolURL,
	}

	ppme.On("Handle", "POST", "/profiles", httpmock.JSONMatcher(&msgReq)).Return(httpmock.Response{
		Status: 201,
		Body:   httpmock.ToJSON(msgResp),
	})

	id, err := c.SendProfile(context.Background(), "TestClientWithSymbols", strings.NewReader("hellothere"))
	if err != nil {
		t.Fatalf("failed to SendProfile: %v", err)
	}
	if exp, got := msgResp.ID, id; exp != got {
		t.Errorf("returned ID not as expected: exp: %v, got: %v", exp, got)
	}

	ppme.AssertExpectations(t)
}

func TestClientUploadsBinary(t *testing.T) {
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
		ID:                "23E3490F-9F1F-4A19-9EBB-07592A7A1ED0",
		BinaryNeedsUpload: true,
	}
	msgReq := msg.ProfilePostRequest{
		Profile:    []byte("hellothere"),
		BinaryName: filepath.Base(os.Args[0]),
		BinaryMD5:  binaryMD5,
	}

	ppme.On("Handle", "POST", "/profiles", httpmock.JSONMatcher(&msgReq)).Return(httpmock.Response{
		Status: 201,
		Body:   httpmock.ToJSON(msgResp),
	})

	bin, err := ioutil.ReadFile(os.Args[0])
	if err != nil {
		t.Fatalf("failed to read binary: %v", err)
	}

	binMatch := mock.MatchedBy(func(arg []byte) bool { return reflect.DeepEqual(arg, bin) })
	ppme.On("Handle", "PUT", fmt.Sprintf("/binaries/%s", binaryMD5), binMatch).Return(httpmock.Response{
		Status: 204,
	})

	id, err := c.SendProfile(context.Background(), "TestClientUploadsBinary", strings.NewReader("hellothere"))
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

	_, err = c.SendProfile(context.Background(), "TestSendProfileReturnsErrorOnNon201Response", strings.NewReader("hellothere"))
	if err == nil {
		t.Errorf("expected SendProfile to return error but it didn't")
	}

	ppme.AssertExpectations(t)
}

func TestSendProfileReturnsErrorOnNon204OnUploadBinary(t *testing.T) {
	ppme := &httpmock.MockHandler{}

	s := httpmock.NewServer(ppme)
	defer s.Close()

	c, err := New(s.URL(), nil)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	msgResp := msg.ProfilePostResponse{
		ID:                "23E3490F-9F1F-4A19-9EBB-07592A7A1ED0",
		BinaryNeedsUpload: true,
	}
	msgReq := msg.ProfilePostRequest{
		Profile:    []byte("hellothere"),
		BinaryName: filepath.Base(os.Args[0]),
		BinaryMD5:  binaryMD5,
	}

	ppme.On("Handle", "POST", "/profiles", httpmock.JSONMatcher(&msgReq)).Return(httpmock.Response{
		Status: 201,
		Body:   httpmock.ToJSON(msgResp),
	})

	ppme.On("Handle", "PUT", fmt.Sprintf("/binaries/%s", binaryMD5), mock.Anything).Return(httpmock.Response{
		Status: 500,
	})

	id, err := c.SendProfile(context.Background(), "TestSendProfileReturnsErrorOnNon204OnUploadBinary", strings.NewReader("hellothere"))
	if err == nil {
		t.Error("expected SendProfile to return error but it didn't")
	}
	if exp, got := msgResp.ID, id; exp != got {
		t.Errorf("returned ID not as expected: exp: %v, got: %v", exp, got)
	}

	ppme.AssertExpectations(t)
}
