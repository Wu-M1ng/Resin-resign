package state

import (
	"errors"
	"testing"

	"github.com/Resinat/Resin/internal/feed"
	"github.com/Resinat/Resin/internal/model"
)

func testFeed(t *testing.T, id, name string) model.SubscriptionFeed {
	t.Helper()
	plain, hash, prefix, err := feed.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	_ = plain
	return model.SubscriptionFeed{
		ID: id, Name: name, PlatformID: "platform-1", DefaultFormat: "clash-meta",
		EnabledFormatsJSON: `["clash-meta","singbox"]`, UnsupportedPolicy: "skip",
		RelayEnabled: true, RelayHost: "resin.example.com", RelayPort: 2261,
		Pretty: true, Enabled: true, TokenHash: hash, TokenPrefix: prefix,
		CreatedAtNs: 100, UpdatedAtNs: 200,
	}
}

func TestFeedRepoCRUDAndTokenLookup(t *testing.T) {
	repo := newTestStateRepo(t)
	want := testFeed(t, "feed-1", "All nodes")
	if err := repo.InsertFeed(want); err != nil {
		t.Fatalf("InsertFeed: %v", err)
	}

	got, err := repo.GetFeed(want.ID)
	if err != nil {
		t.Fatalf("GetFeed: %v", err)
	}
	if got.ID != want.ID || got.Name != want.Name || !got.Pretty || !got.Enabled || !got.RelayEnabled || got.RelayHost != want.RelayHost || got.RelayPort != want.RelayPort {
		t.Fatalf("GetFeed = %+v, want %+v", got, want)
	}
	if got.TokenHash != want.TokenHash || got.TokenPrefix != want.TokenPrefix {
		t.Fatal("token metadata did not round-trip")
	}

	feeds, err := repo.ListFeeds()
	if err != nil || len(feeds) != 1 {
		t.Fatalf("ListFeeds = %d, err=%v; want one feed", len(feeds), err)
	}
	byHash, err := repo.FindEnabledByTokenHash(want.TokenHash)
	if err != nil || byHash.ID != want.ID {
		t.Fatalf("FindEnabledByTokenHash = %+v, err=%v", byHash, err)
	}
	if _, err := repo.FindEnabledByTokenHash(want.TokenPrefix); !errors.Is(err, ErrNotFound) {
		t.Fatalf("prefix lookup error = %v, want ErrNotFound", err)
	}

	want.Name = "Updated nodes"
	want.Enabled = false
	want.Pretty = false
	want.UpdatedAtNs = 300
	if err := repo.UpdateFeed(want); err != nil {
		t.Fatalf("UpdateFeed: %v", err)
	}
	got, err = repo.GetFeed(want.ID)
	if err != nil {
		t.Fatalf("GetFeed after update: %v", err)
	}
	if got.Name != want.Name || got.Enabled || got.Pretty || got.CreatedAtNs != 100 || got.UpdatedAtNs != 300 {
		t.Fatalf("updated feed = %+v", got)
	}
	if _, err := repo.FindEnabledByTokenHash(want.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled feed lookup error = %v, want ErrNotFound", err)
	}

	if err := repo.DeleteFeed(want.ID); err != nil {
		t.Fatalf("DeleteFeed: %v", err)
	}
	if _, err := repo.GetFeed(want.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetFeed after delete error = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteFeed(want.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DeleteFeed error = %v, want ErrNotFound", err)
	}
}

func TestFeedRepoEnforcesUniqueNameAndToken(t *testing.T) {
	repo := newTestStateRepo(t)
	first := testFeed(t, "feed-1", "same")
	if err := repo.InsertFeed(first); err != nil {
		t.Fatal(err)
	}
	second := testFeed(t, "feed-2", "same")
	if err := repo.InsertFeed(second); err == nil {
		t.Fatal("expected duplicate name insert to fail")
	}
	second.Name = "other"
	second.TokenHash = first.TokenHash
	if err := repo.InsertFeed(second); err == nil {
		t.Fatal("expected duplicate token hash insert to fail")
	}
}
