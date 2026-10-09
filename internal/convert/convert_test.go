package convert

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/api/v1/workflows/html2md" || r.Header.Get("Content-Type") != "application/json" ||
			strings.TrimSpace(string(got)) != `{"html":"<p>x</p>"}` {
			t.Errorf("request %s %s %q", r.Method, r.URL.Path, got)
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", time.Second)
}

func TestConvert(t *testing.T) {
	c := serve(t, 200, `{"title":" T ","published_date":"2026-01-02","slug":"a-b-1","markdown":"# T","session_id":"s"}`)
	a, err := c.Convert(context.Background(), []byte("<p>x</p>"))
	if err != nil || a != (Article{Title: "T", PublishedDate: "2026-01-02", Slug: "a-b-1", Markdown: "# T"}) {
		t.Fatalf("article %+v, err %v", a, err)
	}
}

func TestConvertRejectsUnsafeOutput(t *testing.T) {
	for _, body := range []string{
		`{"title":"T","published_date":"2026-01-02","slug":"../etc","markdown":"x"}`,
		`{"title":"T","published_date":"2026-01-02","slug":"A","markdown":"x"}`,
		`{"title":"T","published_date":"2026-1-2","slug":"a","markdown":"x"}`,
		`{"title":"T","published_date":"2026-02-30","slug":"a","markdown":"x"}`,
		`{"title":"","published_date":"2026-01-02","slug":"a","markdown":"x"}`,
		`{"title":"T","published_date":"2026-01-02","slug":"a","markdown":" "}`,
		`not json`,
	} {
		if _, err := serve(t, 200, body).Convert(context.Background(), []byte("<p>x</p>")); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestConvertReportsServiceError(t *testing.T) {
	_, err := serve(t, 502, `{"error":"wf html2md 失败: boom","code":"workflow_failed"}`).Convert(context.Background(), []byte("<p>x</p>"))
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}
