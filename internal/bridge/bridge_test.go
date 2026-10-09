package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeWf behaves like `wf html2md index.html result.json` according to the
// html it is given.
const fakeWf = `#!/bin/sh
[ "$1" = html2md ] && [ "$2" = index.html ] && [ "$3" = result.json ] || { echo "bad args: $*" >&2; exit 2; }
case "$(cat index.html)" in
  *fail*) echo "model exploded" >&2; exit 1 ;;
  *slow*) sleep 5 ;;
  *nojson*) echo "not json" > result.json; exit 0 ;;
esac
printf '{"title":"T","published_date":"2026-01-02","slug":"t","markdown":"# T\\n"}' > result.json
`

func newBridge(t *testing.T, timeout time.Duration) (*Bridge, string) {
	t.Helper()
	dir := t.TempDir()
	wf := filepath.Join(dir, "wf")
	if err := os.WriteFile(wf, []byte(fakeWf), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	b, err := New(Config{Wf: wf, WorkDir: work, Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	return b, work
}

func post(b *Bridge, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/convert", strings.NewReader(body)))
	return rec
}

func jobs(t *testing.T, work string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func errorOf(rec *httptest.ResponseRecorder) string {
	var body struct{ Error string }
	json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Error
}

func TestConvertSucceeds(t *testing.T) {
	b, work := newBridge(t, 10*time.Second)
	rec := post(b, "<p>hello</p>")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"slug":"t"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if n := len(jobs(t, work)); n != 0 {
		t.Fatalf("%d work dirs left after success", n)
	}
}

func TestConvertFailureKeepsWorkDir(t *testing.T) {
	b, work := newBridge(t, 10*time.Second)
	rec := post(b, "<p>fail</p>")
	if rec.Code != http.StatusBadGateway || !strings.Contains(errorOf(rec), "model exploded") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	entries := jobs(t, work)
	if len(entries) != 1 {
		t.Fatalf("work dirs = %v", entries)
	}
	if html, _ := os.ReadFile(filepath.Join(work, entries[0].Name(), "index.html")); string(html) != "<p>fail</p>" {
		t.Fatalf("kept html = %q", html)
	}
	if rec := post(b, "<p>nojson</p>"); rec.Code != http.StatusBadGateway || !strings.Contains(errorOf(rec), "JSON") {
		t.Fatalf("invalid output: status %d body %s", rec.Code, rec.Body)
	}
}

func TestConvertTimeout(t *testing.T) {
	b, _ := newBridge(t, 300*time.Millisecond)
	start := time.Now()
	rec := post(b, "<p>slow</p>")
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("timeout took %v; wf was not stopped", took)
	}
}

func TestConvertIsSerial(t *testing.T) {
	b, _ := newBridge(t, 10*time.Second)
	b.slot <- struct{}{} // a conversion is running
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/convert", strings.NewReader("<p>x</p>")).WithContext(ctx))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	<-b.slot
	if rec := post(b, "<p>x</p>"); rec.Code != http.StatusOK {
		t.Fatalf("after slot freed: status %d", rec.Code)
	}
}

func TestConvertRejectsEmptyBody(t *testing.T) {
	b, _ := newBridge(t, time.Second)
	if rec := post(b, "  "); rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestNewRemovesStaleWorkDirs(t *testing.T) {
	work := t.TempDir()
	stale, fresh := filepath.Join(work, "job-old"), filepath.Join(work, "job-new")
	for _, d := range []string{stale, fresh} {
		os.Mkdir(d, 0o700)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(stale, old, old)
	if _, err := New(Config{Wf: "wf", WorkDir: work, Timeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale dir kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh dir removed")
	}
}
