// Command wf-posts converts cleaned html to Markdown by running
// `wf html2md`, one request at a time.
//
// It runs as a systemd user service of the user whose wf and Codex login it
// uses, and listens where only the Docker host and its containers reach it.
// There is no authentication.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"posts/internal/bridge"
)

func main() {
	listen := flag.String("listen", "172.28.1.1:55680", "HTTP listen address")
	wf := flag.String("wf", "wf", "wf executable")
	workDir := flag.String("workdir", "/tmp/wf-posts", "directory for per-conversion work directories")
	timeout := flag.Duration("timeout", 30*time.Minute, "limit for one conversion, including queueing")
	flag.Parse()

	if err := run(*listen, bridge.Config{Wf: *wf, WorkDir: *workDir, Timeout: *timeout}); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(listen string, cfg bridge.Config) error {
	b, err := bridge.New(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	// Request contexts derive from ctx, so a signal stops a running wf.
	srv := &http.Server{
		Handler:           b.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", ln.Addr().String(), "wf", cfg.Wf, "workdir", cfg.WorkDir)
		errc <- srv.Serve(ln)
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
