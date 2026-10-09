package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validOIDC = `
public_url: https://posts.test
oidc:
  issuer: https://auth.test
  client_id: posts
  client_secret: secret
`

func TestLoadRequiresAuthentication(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || !strings.Contains(err.Error(), "public_url") {
		t.Fatalf("err = %v, want public_url error", err)
	}
}

func TestLoadOIDC(t *testing.T) {
	path := write(t, "listen: \":9000\"\n"+validOIDC)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Listen:      ":9000",
		Database:    filepath.Join(filepath.Dir(path), "posts.db"),
		DataDir:     filepath.Join(filepath.Dir(path), "data"),
		PublicURL:   "https://posts.test",
		OIDC:        OIDC{Issuer: "https://auth.test", ClientID: "posts", ClientSecret: "secret"},
		Workflow:    Workflow{URL: "http://127.0.0.1:55680", Timeout: Duration(30 * time.Minute)},
		MaxUploadMB: 100,
	}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadPaths(t *testing.T) {
	cfg, err := Load(write(t, "dev_user: eric\ndatabase: /var/lib/posts.db\ndata_dir: /srv/posts\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database != "/var/lib/posts.db" || cfg.DataDir != "/srv/posts" {
		t.Fatalf("absolute paths = %q, %q", cfg.Database, cfg.DataDir)
	}
	path := write(t, "dev_user: eric\ndatabase: db/posts.db\ndata_dir: posts\n")
	if cfg, err = Load(path); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if want := filepath.Join(dir, "db", "posts.db"); cfg.Database != want {
		t.Fatalf("relative database = %q, want %q", cfg.Database, want)
	}
	if want := filepath.Join(dir, "posts"); cfg.DataDir != want {
		t.Fatalf("relative data_dir = %q, want %q", cfg.DataDir, want)
	}
}

func TestLoadClientSecretFromEnv(t *testing.T) {
	t.Setenv(ClientSecretEnv, "from-env")
	cfg, err := Load(write(t, strings.Replace(validOIDC, "  client_secret: secret\n", "", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OIDC.ClientSecret != "from-env" {
		t.Fatalf("secret = %q", cfg.OIDC.ClientSecret)
	}
}

func TestLoadDevUserNeedsNoOIDC(t *testing.T) {
	if _, err := Load(write(t, "dev_user: eric\n")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	// A misplaced dev_user under oidc must not silently fall back to OIDC.
	_, err := Load(write(t, validOIDC+"  dev_user: eric\n"))
	if err == nil || !strings.Contains(err.Error(), "dev_user") {
		t.Fatalf("err = %v, want unknown dev_user error", err)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	for _, content := range []string{
		"data_dir: \"\"\ndev_user: eric\n",
		"max_upload_mb: 0\ndev_user: eric\n",
		"workflow:\n  url: 172.28.1.1:55680\ndev_user: eric\n",
		strings.Replace(validOIDC, "https://posts.test", "https://posts.test/sub", 1),
		strings.Replace(validOIDC, "https://posts.test", "posts.test", 1),
		strings.Replace(validOIDC, "  issuer: https://auth.test\n", "", 1),
	} {
		if _, err := Load(write(t, content)); err == nil {
			t.Errorf("expected error for:\n%s", content)
		}
	}
}

func TestLoadTimeout(t *testing.T) {
	cfg, err := Load(write(t, "dev_user: eric\nworkflow:\n  timeout: 1h30m\n"))
	if err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfg.Workflow.Timeout) != 90*time.Minute {
		t.Fatalf("timeout = %v", time.Duration(cfg.Workflow.Timeout))
	}
	for _, in := range []string{"0s", "5", "500ms", "-5m", "abc"} {
		if _, err := Load(write(t, "dev_user: eric\nworkflow:\n  timeout: "+in+"\n")); err == nil || !strings.Contains(err.Error(), "duration") {
			t.Errorf("%s: err = %v, want duration error", in, err)
		}
	}
}
