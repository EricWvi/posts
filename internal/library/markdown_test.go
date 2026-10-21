package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"posts/internal/store"
)

func TestSaveMarkdownBeforeImport(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := convertedPost(t, e)
	file, _ := e.lib.MarkdownFile(p)
	updated := []byte("# 手动编辑\n\n修改后的正文。\n")
	if err := e.lib.SaveMarkdown(ctx, e.user+1, p.ID, updated); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign save: %v", err)
	}
	ready, err := e.lib.Upload(ctx, e.user, page, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.lib.SaveMarkdown(ctx, e.user, ready.ID, updated); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("unconverted save: %v", err)
	}
	if err := e.lib.SaveMarkdown(ctx, e.user, p.ID, []byte{0xff}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid UTF-8: %v", err)
	}
	if err := e.lib.SaveMarkdown(ctx, e.user, p.ID, updated); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != string(updated) {
		t.Fatalf("saved %q, %v", got, err)
	}
	after, err := e.st.Post(ctx, e.user, p.ID)
	if err != nil || after.Path != p.Path || after.Title != p.Title || !after.ConvertedAt.Equal(*p.ConvertedAt) {
		t.Fatalf("metadata changed: %+v, %v", after, err)
	}
	original, err := os.ReadFile(filepath.Join(e.lib.postDir(p), "original.html"))
	if err != nil || string(original) != string(page) {
		t.Fatalf("original changed: %v", err)
	}
	importer := &fakeImporter{err: errors.New("offline")}
	e.lib.importer = importer
	if err := e.lib.Import(ctx, e.user, p.ID, p.ImportMetadata()); err != nil {
		t.Fatal(err)
	}
	if err := e.lib.SaveMarkdown(ctx, e.user, p.ID, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("queued save: %v", err)
	}
	claimed, err := e.st.ClaimImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.lib.SaveMarkdown(ctx, e.user, p.ID, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("importing save: %v", err)
	}
	e.lib.importPost(ctx, claimed)
	if err := e.lib.SaveMarkdown(ctx, e.user, p.ID, nil); err != nil {
		t.Fatalf("failed import edit: %v", err)
	}
	importer.err = nil
	if err := e.lib.Import(ctx, e.user, p.ID, p.ImportMetadata()); err != nil {
		t.Fatal(err)
	}
	claimed, err = e.st.ClaimImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e.lib.importPost(ctx, claimed)
	got, err = os.ReadFile(filepath.Join(importer.dir, "hello.md"))
	if err != nil || len(got) != 0 {
		t.Fatalf("import did not use edited file: %q, %v", got, err)
	}
	if err := e.lib.SaveMarkdown(ctx, e.user, p.ID, updated); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("imported save: %v", err)
	}
}
