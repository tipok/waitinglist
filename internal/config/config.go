package config

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	DefaultPort                  = 8080
	DefaultDatabaseURL           = "postgres://localhost:5432/waitinglist?sslmode=disable"
	DefaultMigrationsDir         = "migrations"
	DefaultEntryBatchSize        = 25
	DefaultEntryWindowInterval   = 30 * time.Hour
	DefaultWaitlistCheckInterval = 1 * time.Hour
)

type Config struct {
	Port              int                     `koanf:"port"`
	Database          DatabaseConfig          `koanf:"database"`
	Waitlist          WaitlistConfig          `koanf:"waitlist"`
	SchedulerInterval SchedulerIntervalConfig `koanf:"schedulerInterval"`
	Admin             AdminConfig             `koanf:"admin"`
}

// AdminConfig configures the protected /admin/* routes. When BasicAuth is
// missing or empty, the admin routes are not registered.
type AdminConfig struct {
	BasicAuth BasicAuthConfig `koanf:"basicAuth"`
}

// BasicAuthConfig holds the credentials for HTTP Basic Auth on /admin/*.
// PasswordHash must be a bcrypt hash; generate with e.g.
//
//	htpasswd -nbBC 10 admin 'changeme' | cut -d: -f2
type BasicAuthConfig struct {
	Username     string `koanf:"username"`
	PasswordHash string `koanf:"passwordHash"`
}

type DatabaseConfig struct {
	URL           string `koanf:"url"`
	Username      string `koanf:"username"`
	Password      string `koanf:"password"`
	MigrationsDir string `koanf:"migrationsDir"`
}

type WaitlistConfig struct {
	EntryBatchSize      int           `koanf:"entryBatchSize"`
	EntryWindowInterval time.Duration `koanf:"entryWindowInterval"`
}

type SchedulerIntervalConfig struct {
	Disabled              bool          `koanf:"disabled"`
	WaitlistCheckInterval time.Duration `koanf:"waitlistCheckInterval"`
}

// Flags holds the parsed CLI arguments for the server binary.
type Flags struct {
	ConfigPath  string
	HealthCheck bool
	Port        int // 0 means not set; only used in --health-check mode
}

// ParseFlags parses CLI arguments and returns the resolved Flags.
func ParseFlags(args []string) (Flags, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", "conf/dev.json", "path to JSON configuration file")
	healthCheck := fs.Bool("health-check", false, "probe /healthz and exit 0/1 (for Docker HEALTHCHECK)")
	port := fs.Int("port", 0, "port to probe in --health-check mode (overrides WL_PORT env and default)")

	if err := fs.Parse(args); err != nil {
		return Flags{}, fmt.Errorf("parsing flags: %w", err)
	}

	if *configPath == "" {
		return Flags{}, fmt.Errorf("--config flag is required")
	}

	return Flags{ConfigPath: *configPath, HealthCheck: *healthCheck, Port: *port}, nil
}

// Load reads the configuration from the given JSON file path and returns a Config
// with defaults applied for any missing fields.
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	if err := loadFileConfig(path, k); err != nil {
		return nil, fmt.Errorf("loading config file: %w", err)
	}

	if err := loadEnvConfig(k); err != nil {
		return nil, fmt.Errorf("loading environment variables: %w", err)
	}

	cfg := &Config{
		Port: DefaultPort,
		Database: DatabaseConfig{
			URL:           DefaultDatabaseURL,
			MigrationsDir: DefaultMigrationsDir,
		},
		Waitlist: WaitlistConfig{
			EntryBatchSize:      DefaultEntryBatchSize,
			EntryWindowInterval: DefaultEntryWindowInterval,
		},
		SchedulerInterval: SchedulerIntervalConfig{
			Disabled:              false,
			WaitlistCheckInterval: DefaultWaitlistCheckInterval,
		},
	}

	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Database.URL == "" {
		cfg.Database.URL = DefaultDatabaseURL
	}
	if cfg.Database.MigrationsDir == "" {
		cfg.Database.MigrationsDir = DefaultMigrationsDir
	}
	if cfg.Waitlist.EntryBatchSize == 0 {
		cfg.Waitlist.EntryBatchSize = DefaultEntryBatchSize
	}
	if cfg.Waitlist.EntryWindowInterval == 0 {
		cfg.Waitlist.EntryWindowInterval = DefaultEntryWindowInterval
	}
	if cfg.SchedulerInterval.WaitlistCheckInterval == 0 {
		cfg.SchedulerInterval.WaitlistCheckInterval = DefaultWaitlistCheckInterval
	}

	return cfg, nil
}

func loadFileConfig(path string, k *koanf.Koanf) error {
	return k.Load(file.Provider(path), json.Parser())
}

func loadEnvConfig(k *koanf.Koanf) error {
	return k.Load(env.Provider(".", env.Opt{
		Prefix: "WL_",
		TransformFunc: func(k, v string) (string, any) {
			k = strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(k, "WL_")), "_", ".")
			if strings.Contains(v, " ") {
				return k, strings.Split(v, " ")
			}

			return k, v
		},
	}), nil)
}
