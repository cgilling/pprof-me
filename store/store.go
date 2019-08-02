package store

import (
	"context"
	"time"

	"github.com/cgilling/pprof-me/msg"
)

type ProfileStore interface {
	CreateID(ctx context.Context, appName string) string
	ListProfiles(ctx context.Context) ([]msg.ProfileListInfo, error)
	StoreProfile(ctx context.Context, id string, profile []byte, meta ProfileMetadata) error
	GetProfile(ctx context.Context, id string) (profile []byte, meta ProfileMetadata, err error)
}

type ListProfilesFilter struct {
	StartTime time.Time
	EndTime   time.Time
}

type ProfileMetadata struct {
	AppName   string
	Version   string
	BinaryMD5 string
	Timestamp time.Time
}
