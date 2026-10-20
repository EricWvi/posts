package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"posts/internal/auth"
	"posts/internal/convert"
	"posts/internal/library"
	"posts/internal/store"
)

type fakeConverter struct{ article convert.Article }

func (f fakeConverter) Convert(context.Context, []byte) (convert.Article, error) {
	return f.article, nil
}

const photo = "55c64d0fcd6f9d5f.png" // sha256("photo")

var page = `<html><head><title>Hello | Blog</title></head><body><h1>Hello</h1><p>Real text.</p>
<img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString([]byte("photo")) + `"></body></html>`

func newServer(t *testing.T, options ...library.Option) (http.Handler, *store.Store) {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "posts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	conv := fakeConverter{convert.Article{Title: "Hello", PublishedDate: "2025-03-04", Slug: "hello",
		Markdown: "# Hello\n\nReal text.\n\n![](assets/" + photo + ")\n"}}
	lib, err := library.New(context.Background(), st, filepath.Join(root, "data"), conv, options...)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { lib.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	web := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}}
	h, err := New(st, lib, auth.New(st, auth.Config{DevUser: "eric"}), web, Options{Version: "test", MaxUploadBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return h, st
}

func do(h http.Handler, method, path string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	if body == nil {
		body = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func upload(t *testing.T, h http.Handler, files map[string]string) uploadResult {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for name, content := range files {
		fw, _ := mw.CreateFormFile("files", name)
		fw.Write([]byte(content))
	}
	mw.Close()
	rec := do(h, "POST", "/api/posts", &body, mw.FormDataContentType())
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var res uploadResult
	json.Unmarshal(rec.Body.Bytes(), &res)
	return res
}

func waitDone(t *testing.T, h http.Handler, id int64) store.Post {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		var p store.Post
		json.Unmarshal(do(h, "GET", "/api/posts/"+itoa(id), nil, "").Body.Bytes(), &p)
		if p.Status == store.StatusDone {
			return p
		}
	}
	t.Fatalf("post %d not converted", id)
	return store.Post{}
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestUploadConvertReadAndDelete(t *testing.T) {
	h, _ := newServer(t)
	res := upload(t, h, map[string]string{"hello.html": page, "notes.txt": "x"})
	if len(res.Posts) != 1 || len(res.Errors) != 1 || res.Errors[0].File != "notes.txt" {
		t.Fatalf("upload result = %+v", res)
	}
	id := res.Posts[0].ID
	if res.Posts[0].Status != store.StatusReady {
		t.Fatalf("upload status = %s", res.Posts[0].Status)
	}
	raw := do(h, "GET", "/api/posts/"+itoa(id)+"/html", nil, "")
	if raw.Code != 200 || raw.Body.String() != page || !strings.Contains(raw.Header().Get("Content-Security-Policy"), "sandbox;") {
		t.Fatalf("original: %d %s", raw.Code, raw.Body)
	}
	if rec := do(h, "POST", "/api/posts/"+itoa(id)+"/convert", nil, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	p := waitDone(t, h, id)
	if p.Title != "Hello" || p.Path != "2025/03/"+itoa(id)+"/hello.md" || !p.OriginalAvailable {
		t.Fatalf("post = %+v", p)
	}
	raw = do(h, "GET", "/api/posts/"+itoa(id)+"/html", nil, "")
	if raw.Code != 200 || raw.Body.String() != page || !strings.Contains(raw.Header().Get("Content-Security-Policy"), "sandbox;") {
		t.Fatalf("converted original: %d %s", raw.Code, raw.Body)
	}

	var list []store.Post
	json.Unmarshal(do(h, "GET", "/api/posts", nil, "").Body.Bytes(), &list)
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("list = %+v", list)
	}
	md := do(h, "GET", "/api/posts/"+itoa(id)+"/md", nil, "")
	if md.Code != 200 || !strings.HasPrefix(md.Body.String(), "# Hello") ||
		!strings.HasPrefix(md.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("md: %d %q %s", md.Code, md.Body, md.Header())
	}
	asset := do(h, "GET", "/api/posts/"+itoa(id)+"/assets/"+photo, nil, "")
	if asset.Code != 200 || asset.Body.String() != "photo" || !strings.Contains(asset.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("asset: %d %q", asset.Code, asset.Body)
	}
	if rec := do(h, "GET", "/api/posts/"+itoa(id)+"/assets/..%2Fhello.md", nil, ""); rec.Code != 404 {
		t.Fatalf("traversal: %d", rec.Code)
	}
	zip := do(h, "GET", "/api/posts/"+itoa(id)+"/zip", nil, "")
	if zip.Code != 200 || !bytes.HasPrefix(zip.Body.Bytes(), []byte("PK")) ||
		!strings.Contains(zip.Header().Get("Content-Disposition"), "hello.zip") {
		t.Fatalf("zip: %d %s", zip.Code, zip.Header())
	}
	if rec := do(h, "POST", "/api/posts/"+itoa(id)+"/retry", nil, ""); rec.Code != http.StatusConflict {
		t.Fatalf("retry done post: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/posts/"+itoa(id), nil, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/posts/"+itoa(id), nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted: %d", rec.Code)
	}
}

type fakeImporter struct{}

func (fakeImporter) Upload(context.Context, string) (string, error) {
	return "0199f0a0-0000-7000-8000-000000000000", nil
}

func TestManualPalaceImport(t *testing.T) {
	h, _ := newServer(t, library.WithImporter(fakeImporter{}))
	source := "<!-- Page saved with SingleFile\n url: https://blog.test/hello\n -->" + page
	res := upload(t, h, map[string]string{"hello.html": source})
	id := res.Posts[0].ID
	path := "/api/posts/" + itoa(id)
	if rec := do(h, "POST", path+"/import", nil, ""); rec.Code != 409 {
		t.Fatalf("import before conversion: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", path+"/convert", nil, ""); rec.Code != 204 {
		t.Fatalf("start: %d", rec.Code)
	}
	p := waitDone(t, h, id)
	if p.PalaceStatus != "" || p.PalaceID != "" {
		t.Fatalf("automatically imported: %+v", p)
	}
	if rec := do(h, "POST", path+"/import", nil, ""); rec.Code != 204 {
		t.Fatalf("import: %d %s", rec.Code, rec.Body)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if err := json.Unmarshal(do(h, "GET", path, nil, "").Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.PalaceStatus == "done" {
			if p.PalaceID == "" || p.Status != store.StatusDone {
				t.Fatalf("post = %+v", p)
			}
			if rec := do(h, "POST", path+"/import", nil, ""); rec.Code != 409 {
				t.Fatalf("duplicate import: %d", rec.Code)
			}
			if rec := do(h, "GET", path+"/md", nil, ""); rec.Code != 200 {
				t.Fatalf("md after import: %d", rec.Code)
			}
			return
		}
	}
	t.Fatalf("import did not complete: %+v", p)
}

func TestUploadRejectsOversizedFiles(t *testing.T) {
	h, _ := newServer(t)
	res := upload(t, h, map[string]string{"big.html": strings.Repeat("x", 1<<20+1)})
	if len(res.Posts) != 0 || len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error, "MB") {
		t.Fatalf("result = %+v", res)
	}
}

func TestMarkdownOfUnconvertedPostIsConflict(t *testing.T) {
	h, st := newServer(t)
	u, _ := st.UpsertUser(context.Background(), store.Identity{Issuer: "dev", Subject: "eric", Name: "eric"})
	p, _ := st.CreatePost(context.Background(), u.ID, store.NewPost{Title: "t"})
	if rec := do(h, "GET", "/api/posts/"+itoa(p.ID)+"/md", nil, ""); rec.Code != http.StatusConflict {
		t.Fatalf("md: %d %s", rec.Code, rec.Body)
	}
}

func TestAPIAndShell(t *testing.T) {
	h, _ := newServer(t)
	if rec := do(h, "GET", "/api/me", nil, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "eric") {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/nope", nil, ""); rec.Code != 404 {
		t.Fatalf("unknown api: %d", rec.Code)
	}
	if rec := do(h, "GET", "/posts/12", nil, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "app") {
		t.Fatalf("shell: %d", rec.Code)
	}
}

func TestOriginalAndConversionArePrivate(t *testing.T) {
	h, st := newServer(t)
	ctx := context.Background()
	bob, err := st.UpsertUser(ctx, store.Identity{Issuer: "dev", Subject: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.CreatePost(ctx, bob.ID, store.NewPost{Title: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteUpload(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	for _, req := range []struct{ method, suffix string }{
		{"GET", "/html"}, {"POST", "/convert"}, {"POST", "/import"},
	} {
		rec := do(h, req.method, "/api/posts/"+itoa(p.ID)+req.suffix, nil, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d %s", req.suffix, rec.Code, rec.Body)
		}
	}
	got, err := st.Post(ctx, bob.ID, p.ID)
	if err != nil || got.Status != store.StatusReady {
		t.Fatalf("private post modified: %+v, %v", got, err)
	}
}
