package palace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeCLI(t *testing.T, script string) *Client {
	t.Helper()
	file := filepath.Join(t.TempDir(), "fake pal")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Client{command: file, timeout: time.Second}
}

func TestUploadInvokesCLIWithDirectoryAndEnvironment(t *testing.T) {
	t.Setenv("PAL_URL", "https://palace.test")
	t.Setenv("PAL_TOKEN", "private-token")
	dir := filepath.Join(t.TempDir(), "article with spaces")
	t.Setenv("EXPECTED_DIR", dir)
	c := fakeCLI(t, `test "$#" = 3 && test "$1" = article && test "$2" = upload && test "$3" = "$EXPECTED_DIR" || exit 2
test "$PAL_URL" = https://palace.test && test "$PAL_TOKEN" = private-token || exit 3
printf '%s\n' '{"id":"0199f0a0-0000-7000-8000-000000000000","title":"hello"}'
`)
	id, err := c.Upload(context.Background(), dir)
	if err != nil || id != "0199f0a0-0000-7000-8000-000000000000" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
}

func TestUploadErrorsAndInvalidResponses(t *testing.T) {
	t.Setenv("PAL_TOKEN", "private-token")
	for _, tc := range []struct{ name, script, want string }{
		{"failure", "echo 'HTTP 401 private-token' >&2\nexit 1\n", "HTTP 401 [redacted]"},
		{"invalid", "echo nope\n", "文章 ID"},
		{"missing id", "echo '{}'\n", "文章 ID"},
		{"blank id", "echo '{\"id\":\" \"}'\n", "文章 ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := fakeCLI(t, tc.script).Upload(context.Background(), t.TempDir())
			if err == nil || id != "" || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("id = %q, err = %v", id, err)
			}
		})
	}
}

func TestUploadTimeout(t *testing.T) {
	c := fakeCLI(t, "exec sleep 5\n")
	c.timeout = 20 * time.Millisecond
	_, err := c.Upload(context.Background(), t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v", err)
	}
}
