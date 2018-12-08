package app

import (
	"fmt"
	"sync"

	"github.com/cgilling/pprof-me/msg"
)

type ProfileStore interface {
	ListProfiles() ([]msg.ProfileInfo, error)
	StoreProfile(id string, profile []byte) error
	StoreBinaryMD5(id, name, md5 string) error
	GetProfile(id string) (profile []byte, err error)
}

type MemStore struct {
	mu       sync.RWMutex
	profiles map[string][]byte
	binInfo  map[string]binInfo
}

func NewMemStore() *MemStore {
	return &MemStore{
		profiles: make(map[string][]byte),
		binInfo:  make(map[string]binInfo),
	}
}

type binInfo struct {
	BinaryName string
	MD5        string
}

func (ms *MemStore) ListProfiles() ([]msg.ProfileInfo, error) {
	var resp []msg.ProfileInfo
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	for id := range ms.profiles {
		resp = append(resp, msg.ProfileInfo{ID: id})
	}
	return resp, nil
}

func (ms *MemStore) StoreProfile(id string, profile []byte) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.profiles[id] = profile
	return nil
}
func (ms *MemStore) StoreBinaryMD5(id, name, md5Str string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.binInfo[id] = binInfo{BinaryName: name, MD5: md5Str}
	return nil
}

func (ms *MemStore) GetProfile(id string) ([]byte, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	b, ok := ms.profiles[id]
	if !ok {
		return nil, fmt.Errorf("failed to find profile for %q", id)
	}
	return b, nil
}
