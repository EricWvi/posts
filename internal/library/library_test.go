package library

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"posts/internal/convert"
	"posts/internal/singlefile"
	"posts/internal/store"
)

// fakeConverter answers with article, or err, and records what it saw.
type fakeConverter struct {
	mu      sync.Mutex
	article convert.Article
	err     error
	calls   int
}

func (f *fakeConverter) Convert(ctx context.Context, html []byte) (convert.Article, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.article, f.err
}

func (f *fakeConverter) set(a convert.Article, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.article, f.err = a, err
}

type env struct {
	lib  *Library
	st   *store.Store
	conv *fakeConverter
	dir  string
	user int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config", "posts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	u, err := st.UpsertUser(context.Background(), store.Identity{Issuer: "i", Subject: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	conv := &fakeConverter{}
	dir := filepath.Join(root, "data")
	lib, err := New(context.Background(), st, dir, conv)
	if err != nil {
		t.Fatal(err)
	}
	return &env{lib: lib, st: st, conv: conv, dir: dir, user: u.ID}
}

func (e *env) run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.lib.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func (e *env) wait(t *testing.T, id int64, want store.Status) store.Post {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		p, err := e.st.Post(context.Background(), e.user, id)
		if err != nil {
			t.Fatal(err)
		}
		if p.Status == want {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatalf("post %d is %s, want %s", id, p.Status, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

const logoName, photoName = "3598ce6f965b2481.png", "55c64d0fcd6f9d5f.png"

var page = []byte(`<!DOCTYPE html><html><!--
 Page saved with SingleFile
 url: https://blog.test/hello
 saved date: Thu Oct 08 2026 10:21:57 GMT+0800 (中国标准时间)
--><head><title>Hello | Blog</title><style>p{}</style></head><body>
<div class=logo><img src="data:image/png;base64,` + b64("logo") + `" alt=Blog></div>
<article><h1>Hello</h1><p>Real text.</p><img src="data:image/png;base64,` + b64("photo") + `" alt=Photo></article>
</body></html>`)

var article = convert.Article{
	Title: "Hello", PublishedDate: "2025-03-04T08:30:00+08:00", Author: "Ada", Slug: "hello",
	Markdown: "# Hello\n\nReal text.\n\n![Photo](assets/" + photoName + ")\n",
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestUploadPreservesOriginalUntilStarted(t *testing.T) {
	e := newEnv(t)
	p, err := e.lib.Upload(context.Background(), e.user, page, "file.html")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != store.StatusReady || p.Title != "Hello | Blog" || p.SourceURL != "https://blog.test/hello" || p.SavedAt == nil {
		t.Fatalf("post = %+v", p)
	}
	file, err := e.lib.OriginalFile(p)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(original, page) {
		t.Fatalf("original changed: %v", err)
	}
	if got := names(t, e.lib.workDir(p.ID)); !slices.Equal(got, []string{"original.html"}) {
		t.Fatalf("upload cleaned prematurely: %v", got)
	}
	if _, err := e.st.ClaimNext(context.Background()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ready post claimed: %v", err)
	}
	if _, err := New(context.Background(), e.st, e.dir, e.conv); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.Post(context.Background(), e.user, p.ID); got.Status != store.StatusReady {
		t.Fatalf("ready state lost after restart: %+v", got)
	}
}

func TestConversionPlacesPostByPublishedDate(t *testing.T) {
	e := newEnv(t)
	e.conv.set(article, nil)
	e.run(t)
	p, _ := e.lib.Upload(context.Background(), e.user, page, "")
	if err := e.lib.Start(context.Background(), e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	done := e.wait(t, p.ID, store.StatusDone)

	rel := "2025/03/" + itoa(p.ID)
	if done.Path != rel+"/hello.md" || done.Title != "Hello" || done.PublishedDate != article.PublishedDate ||
		done.Author != "Ada" || done.Warning != nil {
		t.Fatalf("post = %+v", done)
	}
	md, err := os.ReadFile(filepath.Join(e.dir, rel, "hello.md"))
	if err != nil || string(md) != article.Markdown {
		t.Fatalf("md = %q, %v", md, err)
	}
	// Only the photo the article shows is kept; the site logo is dropped.
	if got := names(t, filepath.Join(e.dir, rel, "assets")); !slices.Equal(got, []string{photoName}) {
		t.Fatalf("assets = %v", got)
	}
	if _, err := os.Stat(e.lib.workDir(p.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("work dir kept after success")
	}
	originalPath, err := e.lib.OriginalFile(done)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(originalPath)
	if err != nil || !bytes.Equal(original, page) {
		t.Fatalf("original not preserved: %v", err)
	}
	if f, err := e.lib.AssetFile(done, photoName); err != nil || !exists(f) {
		t.Fatalf("asset file %q, %v", f, err)
	}
	for _, bad := range []string{"../hello.md", logoName + "/../x", "x.png"} {
		if _, err := e.lib.AssetFile(done, bad); err == nil {
			t.Errorf("asset %q accepted", bad)
		}
	}
}

func TestFailedConversionKeepsWorkAndCanBeRetried(t *testing.T) {
	e := newEnv(t)
	e.conv.set(convert.Article{}, errors.New("workflow 502: boom"))
	e.run(t)
	p, _ := e.lib.Upload(context.Background(), e.user, page, "")
	if err := e.lib.Start(context.Background(), e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	failed := e.wait(t, p.ID, store.StatusFailed)
	if failed.Error != "workflow 502: boom" || !exists(filepath.Join(e.lib.workDir(p.ID), "index.html")) {
		t.Fatalf("post = %+v", failed)
	}
	e.conv.set(article, nil)
	if err := e.lib.Retry(context.Background(), e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, p.ID, store.StatusDone); done.Error != "" {
		t.Fatalf("post = %+v", done)
	}
}

func TestInventedTextIsFlagged(t *testing.T) {
	e := newEnv(t)
	invented := article
	invented.Markdown += "\nA sentence the page never had.\n"
	e.conv.set(invented, nil)
	e.run(t)
	p, _ := e.lib.Upload(context.Background(), e.user, page, "")
	if err := e.lib.Start(context.Background(), e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	done := e.wait(t, p.ID, store.StatusDone)
	if !bytes.Contains(done.Warning, []byte("A sentence the page never had.")) {
		t.Fatalf("warning = %s", done.Warning)
	}
}

func TestDeleteRemovesFiles(t *testing.T) {
	e := newEnv(t)
	e.conv.set(article, nil)
	e.run(t)
	p, _ := e.lib.Upload(context.Background(), e.user, page, "")
	if err := e.lib.Start(context.Background(), e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	done := e.wait(t, p.ID, store.StatusDone)
	originalPath, err := e.lib.OriginalFile(done)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.lib.Delete(context.Background(), e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	if exists(e.lib.postDir(done)) {
		t.Fatal("post dir kept")
	}
	if exists(originalPath) {
		t.Fatal("original kept after deletion")
	}
	if _, err := e.st.Post(context.Background(), e.user, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("post kept: %v", err)
	}
}

func TestZip(t *testing.T) {
	e := newEnv(t)
	e.conv.set(article, nil)
	e.run(t)
	p, _ := e.lib.Upload(context.Background(), e.user, page, "")
	if err := e.lib.Start(context.Background(), e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	done := e.wait(t, p.ID, store.StatusDone)
	var buf bytes.Buffer
	if err := e.lib.WriteZip(&buf, done); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range zr.File {
		got = append(got, f.Name)
	}
	if want := []string{"hello/hello.md", "hello/assets/" + photoName}; !slices.Equal(got, want) {
		t.Fatalf("zip = %v, want %v", got, want)
	}
}

func TestNewDropsAbandonedUploadsAndRequeuesConversions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cut, _ := e.st.CreatePost(ctx, e.user, store.NewPost{Title: "cut"})
	os.MkdirAll(e.lib.workDir(cut.ID), 0o755)
	p, _ := e.lib.Upload(ctx, e.user, page, "")
	if err := e.lib.Start(ctx, e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := New(ctx, e.st, e.dir, e.conv); err != nil {
		t.Fatal(err)
	}
	if exists(e.lib.workDir(cut.ID)) {
		t.Fatal("abandoned work dir kept")
	}
	if _, err := e.st.Post(ctx, e.user, cut.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("abandoned post kept: %v", err)
	}
	if got, _ := e.st.Post(ctx, e.user, p.ID); got.Status != store.StatusQueued {
		t.Fatalf("interrupted post is %s", got.Status)
	}
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestStartChecksOwnershipAndRejectsDuplicateRequests(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, err := e.lib.Upload(ctx, e.user, page, "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.st.UpsertUser(ctx, store.Identity{Issuer: "i", Subject: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.lib.Start(ctx, other.ID, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("another user started conversion: %v", err)
	}
	if err := e.lib.Start(ctx, e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.lib.Start(ctx, e.user, p.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate start accepted: %v", err)
	}
	if err := e.lib.Delete(ctx, e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	if exists(e.lib.workDir(p.ID)) {
		t.Fatal("original kept after deletion")
	}
}

func TestLegacyCleanedPageStillConverts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, err := e.lib.Upload(ctx, e.user, page, "")
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := singlefile.Clean(page)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.lib.writeWork(p.ID, cleaned); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.lib.workDir(p.ID), "original.html")); err != nil {
		t.Fatal(err)
	}
	e.conv.set(article, nil)
	e.run(t)
	if err := e.lib.Start(ctx, e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	e.wait(t, p.ID, store.StatusDone)
}
