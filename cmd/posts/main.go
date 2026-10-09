// Command posts serves the posts archive and its API from a single process
// and converts uploaded pages in the background.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"posts"
	"posts/internal/auth"
	"posts/internal/config"
	"posts/internal/convert"
	"posts/internal/library"
	"posts/internal/server"
	"posts/internal/store"
)

// Set at build time with -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to the YAML config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("posts %s (%s)\n", version, commit)
		return
	}
	if err := run(*configPath); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conv := convert.New(cfg.WfPosts.URL, time.Duration(cfg.WfPosts.Timeout))
	lib, err := library.New(ctx, st, cfg.DataDir, conv)
	if err != nil {
		return err
	}

	if cfg.DevUser != "" {
		slog.Warn("dev_user is set: OIDC is disabled and every request is signed in as this user", "user", cfg.DevUser)
	}
	authn := auth.New(st, auth.Config{
		PublicURL:    cfg.PublicURL,
		Issuer:       cfg.OIDC.Issuer,
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		DevUser:      cfg.DevUser,
	})
	handler, err := server.New(st, lib, authn, posts.Frontend(), server.Options{
		Version:        version,
		MaxUploadBytes: int64(cfg.MaxUploadMB) << 20,
	})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	var worker sync.WaitGroup
	worker.Go(func() { lib.Run(ctx) })
	defer worker.Wait()

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Listen, "database", cfg.Database, "data_dir", cfg.DataDir,
			"wf_posts", cfg.WfPosts.URL, "version", version)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		stop()
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
