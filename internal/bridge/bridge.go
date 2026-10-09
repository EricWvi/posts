// Package bridge exposes `wf html2md` over HTTP so the posts container can
// use the wf installation and Codex login of a user on the Docker host.
package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// maxHTMLBytes caps one cleaned page; SingleFile pages shrink by an order
	// of magnitude once styles, scripts and media are gone.
	maxHTMLBytes = 32 << 20
	// stderrTail is how much of wf's stderr an error response carries.
	stderrTail = 2048
	// keepFailed is how long the work directory of a failed conversion is
	// kept for inspection.
	keepFailed = 7 * 24 * time.Hour
)

// errTimeout marks a request that ran out of Config.Timeout, as opposed to
// one the caller abandoned.
var errTimeout = errors.New("conversion timed out")

// Config configures the bridge.
type Config struct {
	// Wf is the wf executable.
	Wf string
	// WorkDir holds one directory per conversion.
	WorkDir string
	// Timeout bounds one request, including the time it waits for the
	// conversion ahead of it.
	Timeout time.Duration
}

// Bridge runs one conversion at a time.
type Bridge struct {
	cfg  Config
	slot chan struct{}
}

// New prepares the work directory and removes failed conversions older
// than a week.
func New(cfg Config) (*Bridge, error) {
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		return nil, fmt.Errorf("create work dir: %w", err)
	}
	entries, err := os.ReadDir(cfg.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("read work dir: %w", err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && e.IsDir() && time.Since(info.ModTime()) > keepFailed {
			os.RemoveAll(filepath.Join(cfg.WorkDir, e.Name()))
		}
	}
	return &Bridge{cfg: cfg, slot: make(chan struct{}, 1)}, nil
}

// Handler serves POST /api/convert and GET /healthz.
func (b *Bridge) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/convert", b.convert)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	return mux
}

// convert takes the cleaned html as the request body and answers with the
// JSON article wf wrote: title, published_date, slug and markdown.
func (b *Bridge) convert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	html, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHTMLBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "html 超过 32 MB")
			return
		}
		writeError(w, http.StatusBadRequest, "读取请求失败")
		return
	}
	if len(strings.TrimSpace(string(html))) == 0 {
		writeError(w, http.StatusBadRequest, "html 为空")
		return
	}

	ctx, cancel := context.WithTimeoutCause(r.Context(), b.cfg.Timeout, errTimeout)
	defer cancel()
	select {
	case b.slot <- struct{}{}:
		defer func() { <-b.slot }()
	case <-ctx.Done():
		b.fail(w, ctx, "", errors.New("等待前一个转换时超时"))
		return
	}

	dir, err := os.MkdirTemp(b.cfg.WorkDir, "job-")
	if err != nil {
		slog.Error("create job dir", "err", err)
		writeError(w, http.StatusInternalServerError, "无法创建工作目录")
		return
	}
	result, err := b.run(ctx, dir, html)
	if err != nil {
		b.fail(w, ctx, dir, err)
		return
	}
	os.RemoveAll(dir)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(result)
}

func (b *Bridge) run(ctx context.Context, dir string, html []byte) ([]byte, error) {
	if err := os.WriteFile(filepath.Join(dir, "index.html"), html, 0o600); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, b.cfg.Wf, "html2md", "index.html", "result.json")
	cmd.Dir = dir
	// wf starts Codex children; signal the whole group so none outlive it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 10 * time.Second
	var stderr tailBuffer
	cmd.Stderr = &stderr
	started := time.Now()
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("wf 失败: %s", msg)
		}
		return nil, fmt.Errorf("wf 失败: %w", err)
	}
	result, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil {
		return nil, fmt.Errorf("读取 wf 输出: %w", err)
	}
	if !json.Valid(result) {
		return nil, errors.New("wf 输出不是合法 JSON")
	}
	slog.Info("converted", "bytes", len(html), "took", time.Since(started).Round(time.Second))
	return result, nil
}

func (b *Bridge) fail(w http.ResponseWriter, ctx context.Context, dir string, err error) {
	slog.Error("convert failed", "dir", dir, "err", err)
	switch {
	case context.Cause(ctx) == errTimeout:
		writeError(w, http.StatusGatewayTimeout, fmt.Sprintf("转换超过 %s", b.cfg.Timeout))
	case ctx.Err() != nil:
		// The caller went away or the bridge is shutting down.
		writeError(w, http.StatusServiceUnavailable, "转换已取消")
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// tailBuffer keeps the last stderrTail bytes written to it.
type tailBuffer struct{ buf []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - stderrTail; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return strings.ToValidUTF8(string(t.buf), "") }

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
