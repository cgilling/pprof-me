package store

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cgilling/pprof-me/msg"
	"github.com/google/uuid"
)

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
	// TODO: we can switch this func to return an error, can do it with this,
	//       and if an valid appName is not supplied
	uid, _ := uuid.NewUUID()
	id := uid.String()
	ms.mu.Lock()
	ms.meta[id] = ProfileMetadata{AppName: appName}
	ms.mu.Unlock()
	return id
}

func (ms *MemStore) ListProfiles() ([]msg.ProfileListInfo, error) {
	var resp []msg.ProfileListInfo
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	for id := range ms.profiles {
		uid, err := uuid.Parse(id)
		if err != nil {
			log.Println(err)
			// TODO: we should do some logging around this
			continue
		}
		resp = append(resp, msg.ProfileListInfo{
			ID:        id,
			AppName:   ms.meta[id].AppName,
			Timestamp: time.Unix(uid.Time().UnixTime()),
		})
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
