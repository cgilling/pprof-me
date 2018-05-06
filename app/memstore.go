package app

import (
	"fmt"
	"sync"

	"github.com/cgilling/pprof-me/msg"
)

type ProfileStore interface {
	ListProfiles() ([]msg.ProfileInfo, error)
	StoreProfile(id string, profile []byte) error
	StoreSymbols(id string, symbols []byte) error
	StoreBinary(md5sum string, binary []byte) error
	StoreBinaryMD5(id, name, md5 string) error
	GetProfile(id string) (profile []byte, err error)
	GetBinary(id string) (name string, binary []byte, err error)
	GetSymbols(id string) (symbols []byte, err error)
	HasBinaryMD5(md5 string) bool
	HasSymbols(id string) bool
}

type MemStore struct {
	mu       sync.RWMutex
	profiles map[string][]byte
	symbols  map[string][]byte
	binaries map[string][]byte
	binInfo  map[string]binInfo
}

func NewMemStore() *MemStore {
	return &MemStore{
		profiles: make(map[string][]byte),
		symbols:  make(map[string][]byte),
		binaries: make(map[string][]byte),
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
func (ms *MemStore) StoreSymbols(id string, symbols []byte) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.symbols[id] = symbols
	return nil
}
func (ms *MemStore) StoreBinary(md5sum string, binary []byte) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.binaries[md5sum] = binary
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
func (ms *MemStore) GetBinary(id string) (string, []byte, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	info, ok := ms.binInfo[id]
	if !ok {
		return "", nil, fmt.Errorf("failed to find binInfo for %q", id)
	}
	b, ok := ms.binaries[info.MD5]
	if !ok {
		return "", nil, fmt.Errorf("failed to find binary for %q with md5=%q", id, info.MD5)
	}
	return info.BinaryName, b, nil
}
func (ms *MemStore) HasBinaryMD5(md5 string) bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	_, ok := ms.binaries[md5]
	return ok
}

func (ms *MemStore) GetSymbols(id string) ([]byte, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	b, ok := ms.symbols[id]
	if !ok {
		return nil, fmt.Errorf("could not find symbols for %q", id)
	}
	return b, nil
}
func (ms *MemStore) HasSymbols(id string) bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	_, ok := ms.symbols[id]
	return ok
}
