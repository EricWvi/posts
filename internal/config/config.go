// Package config loads the YAML configuration file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// ClientSecretEnv overrides oidc.client_secret so the secret can stay out
// of the config file.
const ClientSecretEnv = "POSTS_OIDC_CLIENT_SECRET"

// Config is the application configuration.
type Config struct {
	// Listen is the HTTP listen address, e.g. ":36751".
	Listen string `yaml:"listen"`
	// Database is the SQLite file. A relative path is resolved against the
	// directory of the config file, so the database sits next to it.
	Database string `yaml:"database"`
	// DataDir holds the converted posts and their assets. A relative path is
	// resolved against the working directory.
	DataDir string `yaml:"data_dir"`
	// PublicURL is the origin browsers use to reach the site, e.g.
	// "https://posts.example.com". It forms the OIDC redirect URI.
	PublicURL string `yaml:"public_url"`
	OIDC      OIDC   `yaml:"oidc"`
	// DevUser, when set, signs every request in as this local user and
	// disables OIDC. For local development only.
	DevUser  string   `yaml:"dev_user"`
	Workflow Workflow `yaml:"workflow"`
	// MaxUploadMB caps the size of one uploaded html file.
	MaxUploadMB int `yaml:"max_upload_mb"`
}

// OIDC configures the identity provider (Authelia).
type OIDC struct {
	Issuer       string `yaml:"issuer"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

// Workflow locates the workflow service on the Docker host, whose
// html2md API converts the pages.
type Workflow struct {
	URL string `yaml:"url"`
	// Timeout bounds one conversion, including the time the service spends
	// queueing and running the model.
	Timeout Duration `yaml:"timeout"`
}

// Duration is a positive duration written with h, m and s units only,
// e.g. "5m", "90s" or "1h30m".
type Duration time.Duration

var durationFormat = regexp.MustCompile(`^(\d+[hms])+$`)

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	parsed, err := time.ParseDuration(node.Value)
	if !durationFormat.MatchString(node.Value) || err != nil || parsed <= 0 {
		return fmt.Errorf("line %d: invalid duration %q, use h, m or s such as \"5m\"", node.Line, node.Value)
	}
	*d = Duration(parsed)
	return nil
}

// Default returns the configuration used when no file is present.
func Default() Config {
	return Config{
		Listen:      ":36751",
		Database:    "posts.db",
		DataDir:     "./data",
		Workflow:    Workflow{URL: "http://127.0.0.1:55680", Timeout: Duration(30 * time.Minute)},
		MaxUploadMB: 100,
	}
}

// Load reads the YAML file at path on top of the defaults.
// A missing file is not an error; the defaults are used. Unknown keys are
// rejected so a misplaced setting such as dev_user cannot be ignored.
func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return cfg, fmt.Errorf("read config: %w", err)
	default:
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	if secret := os.Getenv(ClientSecretEnv); secret != "" {
		cfg.OIDC.ClientSecret = secret
	}
	if cfg.Database != "" && !filepath.IsAbs(cfg.Database) {
		cfg.Database = filepath.Join(filepath.Dir(path), cfg.Database)
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if c.Listen == "" {
		return errors.New("config: listen must not be empty")
	}
	if c.Database == "" {
		return errors.New("config: database must not be empty")
	}
	if c.DataDir == "" {
		return errors.New("config: data_dir must not be empty")
	}
	if c.MaxUploadMB <= 0 {
		return errors.New("config: max_upload_mb must be positive")
	}
	if u, err := url.Parse(c.Workflow.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("config: workflow.url must be an http URL such as http://172.28.1.1:55680")
	}
	if c.DevUser != "" {
		return nil
	}
	u, err := url.Parse(c.PublicURL)
	if c.PublicURL == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") {
		return errors.New("config: public_url must be an origin such as https://posts.example.com")
	}
	if c.OIDC.Issuer == "" || c.OIDC.ClientID == "" || c.OIDC.ClientSecret == "" {
		return fmt.Errorf("config: oidc.issuer, oidc.client_id and oidc.client_secret (or %s) are required unless dev_user is set", ClientSecretEnv)
	}
	return nil
}
