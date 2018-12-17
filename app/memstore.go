package app

import (
	"fmt"
	"sync"

	"github.com/cgilling/pprof-me/msg"
	"github.com/pborman/uuid"
)

type ProfileStore interface {
	CreateID(appName string) string
	ListProfiles() ([]msg.ProfileInfo, error)
	StoreProfile(id string, profile []byte, meta ProfileMetadata) error
	GetProfile(id string) (profile []byte, meta ProfileMetadata, err error)
}

type ProfileMetadata struct {
	AppName   string
	Version   string
	BinaryMD5 string
}

type MemStore struct {
	mu       sync.RWMutex
	profiles map[string][]byte
	meta     map[string]ProfileMetadata
}

func NewMemStore() *MemStore {
	return &MemStore{
		profiles: make(map[string][]byte),
		meta:     make(map[string]ProfileMetadata),
	}
}

func (ms *MemStore) CreateID(appName string) string {
	id := uuid.New()
	ms.mu.Lock()
	ms.meta[id] = ProfileMetadata{AppName: appName}
	ms.mu.Unlock()
	return id
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

func (ms *MemStore) StoreProfile(id string, profile []byte, meta ProfileMetadata) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	meta.AppName = ms.meta[id].AppName
	ms.meta[id] = meta
	ms.profiles[id] = profile
	return nil
}

func (ms *MemStore) GetProfile(id string) ([]byte, ProfileMetadata, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	b, ok := ms.profiles[id]
	if !ok {
		return nil, ProfileMetadata{}, fmt.Errorf("failed to find profile for %q", id)
	}
	return b, ms.meta[id], nil
}
