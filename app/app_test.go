package app

import (
	"context"
	"testing"

	"github.com/cgilling/pprof-me/msg"
	"github.com/cgilling/pprof-me/store"
	"github.com/dghubble/sling"
)

type Fixture struct {
	App *App
}

func NewFixture(t *testing.T) *Fixture {
	app, err := New(Config{})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	go app.ListenAndServe()
	return &Fixture{
		App: app,
	}
}

func (f *Fixture) Cleanup() {
	f.App.Shutdown(context.Background())
}

func TestListProfilesTwoProfiles(t *testing.T) {
	f := NewFixture(t)
	defer f.Cleanup()

	id1 := f.App.profiles.CreateID("testApp")
	id2 := f.App.profiles.CreateID("testApp")

	f.App.profiles.StoreProfile(id1, []byte("profile1"), store.ProfileMetadata{})
	f.App.profiles.StoreProfile(id2, []byte("profile2"), store.ProfileMetadata{})

	sbase := sling.New().Base("http://" + f.App.Addr())
	var lresp msg.ProfileListResponse
	resp, err := sbase.New().Get("profiles").ReceiveSuccess(&lresp)
	if err != nil {
		t.Fatalf("received error listing profiles: %v", err)
	}
	if exp, got := 200, resp.StatusCode; exp != got {
		t.Fatalf("list profiles response code not as expected: exp: %d, got: %d", exp, got)
	}
	if exp, got := 2, len(lresp.Profiles); exp != got {
		t.Fatalf("profile count not as expected: exp: %d, got: %d", exp, got)
	}
	expProfileIDs := map[string]bool{id1: false, id2: false}
	for _, prof := range lresp.Profiles {
		if _, ok := expProfileIDs[prof.ID]; !ok {
			t.Errorf("profile returned with unexpected id: %v", prof.ID)
			continue
		}
		expProfileIDs[prof.ID] = true
	}
	for id, found := range expProfileIDs {
		if found {
			continue
		}
		t.Errorf("did not find ID in profile response: %v", id)
	}
}
