// Command posts serves the posts archive and its API from a single process.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"posts/internal/config"
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
	if _, err := config.Load(*configPath); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
