package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"posts/internal/store"
)

func newService(t *testing.T, cfg Config) (*Service, http.Handler) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "posts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := New(st, cfg)
	mux := http.NewServeMux()
	svc.Register(mux)
	mux.Handle("GET /whoami", svc.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(User(r.Context()))
	})))
	return svc, mux
}

func serve(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestDevUserSignsEveryRequestIn(t *testing.T) {
	_, h := newService(t, Config{DevUser: "eric"})
	rec := serve(h, "GET", "/whoami")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var u store.User
	json.Unmarshal(rec.Body.Bytes(), &u)
	if u.Name != "eric" {
		t.Fatalf("user = %+v", u)
	}
	if rec := serve(h, "GET", "/auth/login"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("login: status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestCallbackWithoutLoginCookieIsRejected(t *testing.T) {
	_, h := newService(t, Config{PublicURL: "https://posts.test", Issuer: "https://idp.invalid", ClientID: "x", ClientSecret: "y"})
	rec := serve(h, "GET", "/auth/callback?code=c&state=s")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			t.Fatalf("session cookie set: %+v", c)
		}
	}
}

func TestLoginReportsUnavailableProvider(t *testing.T) {
	_, h := newService(t, Config{
		PublicURL: "https://posts.test", Issuer: "http://127.0.0.1:1", ClientID: "x", ClientSecret: "y",
	})
	if rec := serve(h, "GET", "/auth/login"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
