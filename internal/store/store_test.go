package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "db", "posts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newUser(t *testing.T, s *Store, subject string) int64 {
	t.Helper()
	u, err := s.UpsertUser(context.Background(), Identity{Issuer: "https://idp.test", Subject: subject})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestUpsertUserKeepsIdentityAndRefreshesProfile(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	first, err := s.UpsertUser(ctx, Identity{Issuer: "i", Subject: "s", Name: "Old", Email: "old@x"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.UpsertUser(ctx, Identity{Issuer: "i", Subject: "s", Name: "New", Email: "new@x"})
	if err != nil {
		t.Fatal(err)
	}
	if second != (User{ID: first.ID, Name: "New", Email: "new@x"}) {
		t.Fatalf("second = %+v, first = %+v", second, first)
	}
	other, _ := s.UpsertUser(ctx, Identity{Issuer: "other", Subject: "s"})
	if other.ID == first.ID {
		t.Fatal("same subject from another issuer must be another user")
	}
}

func TestSessionsLastUntilDeleted(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	uid := newUser(t, s, "alice")
	token, err := s.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.SessionUser(ctx, token)
	if err != nil || u.ID != uid {
		t.Fatalf("user = %+v, err = %v", u, err)
	}
	if err := s.TouchSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SessionUser(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPostLifecycle(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	uid := newUser(t, s, "alice")
	saved := time.Date(2026, 10, 8, 2, 21, 57, 0, time.UTC)
	p, err := s.CreatePost(ctx, uid, NewPost{Title: "Page | Site", SourceURL: "https://x.test/a", SavedAt: saved})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusUploading || p.SavedAt == nil || !p.SavedAt.Equal(saved) || p.ConvertedAt != nil {
		t.Fatalf("created = %+v", p)
	}
	if _, err := s.ClaimNext(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("uploading post was claimed: %v", err)
	}
	if err := s.Enqueue(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimNext(ctx)
	if err != nil || claimed.ID != p.ID || claimed.Status != StatusConverting {
		t.Fatalf("claimed = %+v, err = %v", claimed, err)
	}
	if err := s.Fail(ctx, p.ID, "boom"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Post(ctx, uid, p.ID); got.Status != StatusFailed || got.Error != "boom" {
		t.Fatalf("failed = %+v", got)
	}
	if err := s.Retry(ctx, uid, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Retry(ctx, uid, p.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("retry of queued post: %v", err)
	}
	if _, err := s.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	warning := json.RawMessage(`{"unmatched":["x"]}`)
	if err := s.Finish(ctx, p.ID, Converted{Title: "Page", PublishedDate: "2026-10-01", Slug: "page",
		Path: "2026/10/1/page.md", Warning: warning}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Post(ctx, uid, p.ID)
	if got.Status != StatusDone || got.Title != "Page" || got.Slug != "page" || got.Path != "2026/10/1/page.md" ||
		got.Error != "" || string(got.Warning) != string(warning) || got.ConvertedAt == nil {
		t.Fatalf("done = %+v", got)
	}
	if err := s.Fail(ctx, p.ID, "late"); !errors.Is(err, ErrConflict) {
		t.Fatalf("fail after done: %v", err)
	}
}

func TestClaimNextTakesOldestQueued(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	uid := newUser(t, s, "alice")
	var ids []int64
	for range 3 {
		p, _ := s.CreatePost(ctx, uid, NewPost{Title: "t"})
		s.Enqueue(ctx, p.ID)
		ids = append(ids, p.ID)
	}
	for _, want := range ids {
		if p, err := s.ClaimNext(ctx); err != nil || p.ID != want {
			t.Fatalf("claimed %d (%v), want %d", p.ID, err, want)
		}
	}
	if n, err := s.RequeueConverting(ctx); err != nil || n != 3 {
		t.Fatalf("requeued %d, %v", n, err)
	}
}

func TestDeleteRefusesPostsInProgress(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	uid := newUser(t, s, "alice")
	p, _ := s.CreatePost(ctx, uid, NewPost{Title: "t"})
	if _, err := s.DeletePost(ctx, uid, p.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete uploading: %v", err)
	}
	s.Enqueue(ctx, p.ID)
	s.ClaimNext(ctx)
	if _, err := s.DeletePost(ctx, uid, p.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete converting: %v", err)
	}
	s.Fail(ctx, p.ID, "x")
	if deleted, err := s.DeletePost(ctx, uid, p.ID); err != nil || deleted.ID != p.ID {
		t.Fatalf("delete failed post: %+v %v", deleted, err)
	}
	if _, err := s.Post(ctx, uid, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("post still there: %v", err)
	}
}

func TestUsersCannotSeeOrTouchEachOthersPosts(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	alice, bob := newUser(t, s, "alice"), newUser(t, s, "bob")
	p, _ := s.CreatePost(ctx, alice, NewPost{Title: "secret"})
	s.Enqueue(ctx, p.ID)
	s.ClaimNext(ctx)
	s.Fail(ctx, p.ID, "x")
	if list, _ := s.Posts(ctx, bob); len(list) != 0 {
		t.Fatalf("bob sees %+v", list)
	}
	if _, err := s.Post(ctx, bob, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
	if err := s.Retry(ctx, bob, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retry: %v", err)
	}
	if _, err := s.DeletePost(ctx, bob, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete: %v", err)
	}
	if list, _ := s.Posts(ctx, alice); len(list) != 1 {
		t.Fatalf("alice sees %+v", list)
	}
}

func TestAbandonedUploads(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	uid := newUser(t, s, "alice")
	cut, _ := s.CreatePost(ctx, uid, NewPost{Title: "cut"})
	ok, _ := s.CreatePost(ctx, uid, NewPost{Title: "ok"})
	s.Enqueue(ctx, ok.ID)
	list, err := s.AbandonedUploads(ctx)
	if err != nil || len(list) != 1 || list[0].ID != cut.ID {
		t.Fatalf("abandoned = %+v, %v", list, err)
	}
	if err := s.RemoveAbandoned(ctx, cut.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveAbandoned(ctx, ok.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Posts(ctx, uid); len(list) != 1 || list[0].ID != ok.ID {
		t.Fatalf("posts = %+v", list)
	}
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "posts.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	uid := newUser(t, s, "alice")
	s.CreatePost(context.Background(), uid, NewPost{Title: "kept"})
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if list, _ := s.Posts(context.Background(), uid); len(list) != 1 || list[0].Title != "kept" {
		t.Fatalf("posts = %+v", list)
	}
}
