package store

import (
	"time"

	"github.com/cgilling/pprof-me/msg"
)

type ProfileStore interface {
	CreateID(appName string) string
	ListProfiles() ([]msg.ProfileListInfo, error)
	StoreProfile(id string, profile []byte, meta ProfileMetadata) error
	GetProfile(id string) (profile []byte, meta ProfileMetadata, err error)
}

type ProfileMetadata struct {
	AppName   string
	Version   string
	BinaryMD5 string
	Timestamp time.Time
}
