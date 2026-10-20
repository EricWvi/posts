package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"posts/internal/store"
)

type fakeImporter struct {
	calls int
	dir   string
	err   error
}

func (f *fakeImporter) Upload(_ context.Context, dir string) (string, error) {
	f.calls++
	f.dir = dir
	if f.err != nil {
		return "", f.err
	}
	return "0199f0a0-0000-7000-8000-000000000000", nil
}

func convertedPost(t *testing.T, e *env) store.Post {
	t.Helper()
	ctx := context.Background()
	e.conv.set(article, nil)
	p, err := e.lib.Upload(ctx, e.user, page, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.lib.Start(ctx, e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	p, err = e.st.ClaimNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e.lib.process(ctx, p)
	p, err = e.st.Post(ctx, e.user, p.ID)
	if err != nil || p.Status != store.StatusDone {
		t.Fatalf("post = %+v, %v", p, err)
	}
	return p
}

func TestPalaceImportIsManualAndRetryDoesNotConvert(t *testing.T) {
	e := newEnv(t)
	f := &fakeImporter{err: errors.New("Palace unavailable")}
	e.lib.importer = f
	p := convertedPost(t, e)
	ctx := context.Background()
	if f.calls != 0 || p.PalaceStatus != "" {
		t.Fatal("conversion automatically imported")
	}
	if _, err := e.st.ClaimImport(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unrequested import: %v", err)
	}
	if err := e.lib.Import(ctx, e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.lib.Import(ctx, e.user, p.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate import: %v", err)
	}
	claimed, err := e.st.ClaimImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.lib.Delete(ctx, e.user, p.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("deleted during import: %v", err)
	}
	e.lib.importPost(ctx, claimed)
	failed, _ := e.st.Post(ctx, e.user, p.ID)
	if failed.Status != store.StatusDone || failed.PalaceStatus != "failed" || failed.PalaceError != f.err.Error() {
		t.Fatalf("post = %+v", failed)
	}
	if _, err := e.lib.MarkdownFile(failed); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(f.dir, "article.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["title"] != article.Title || metadata["url"] != p.SourceURL || metadata["date"] != article.PublishedDate || metadata["author"] != article.Author {
		t.Fatalf("metadata = %s", data)
	}
	md, err := os.ReadFile(filepath.Join(f.dir, "hello.md"))
	if err != nil || string(md) != article.Markdown {
		t.Fatalf("Markdown changed: %s, %v", md, err)
	}
	if !exists(filepath.Join(f.dir, "assets", photoName)) {
		t.Fatal("referenced asset missing")
	}
	f.err = nil
	if err := e.lib.Import(ctx, e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err = e.st.ClaimImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e.lib.importPost(ctx, claimed)
	done, _ := e.st.Post(ctx, e.user, p.ID)
	if done.PalaceStatus != "done" || done.PalaceID == "" || done.PalaceError != "" || f.calls != 2 || e.conv.calls != 1 {
		t.Fatalf("post = %+v; imports = %d, conversions = %d", done, f.calls, e.conv.calls)
	}
	if err := e.lib.Import(ctx, e.user, p.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("reimported completed post: %v", err)
	}
}

func TestImportOwnershipAndRestart(t *testing.T) {
	e := newEnv(t)
	e.lib.importer = &fakeImporter{}
	ctx := context.Background()
	p := convertedPost(t, e)
	if err := e.lib.Import(ctx, e.user+1, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign import: %v", err)
	}
	ready, err := e.lib.Upload(ctx, e.user, page, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.lib.Import(ctx, e.user, ready.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("imported HTML: %v", err)
	}
	if err := e.lib.Import(ctx, e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ClaimImport(ctx); err != nil {
		t.Fatal(err)
	}
	lib, err := New(ctx, e.st, e.dir, e.conv, WithImporter(&fakeImporter{}))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := e.st.Post(ctx, e.user, p.ID)
	if got.PalaceStatus != "queued" || got.Status != store.StatusDone {
		t.Fatalf("interrupted import = %+v", got)
	}
	untouched, _ := e.st.Post(ctx, e.user, ready.ID)
	if untouched.PalaceStatus != "" || untouched.Status != store.StatusReady {
		t.Fatalf("untouched = %+v", untouched)
	}
	e.lib = lib
	e.run(t)
	for i := 0; i < 500; i++ {
		got, _ = e.st.Post(ctx, e.user, p.ID)
		if got.PalaceStatus == "done" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker did not resume import: %+v", got)
}

func TestImportMissingSourceKeepsConvertedPost(t *testing.T) {
	e := newEnv(t)
	f := &fakeImporter{}
	e.lib.importer = f
	p := convertedPost(t, e)
	p.SourceURL = ""
	if _, err := e.lib.uploadArticle(context.Background(), p); err == nil || f.calls != 0 {
		t.Fatalf("missing URL: %v, calls %d", err, f.calls)
	}
}
