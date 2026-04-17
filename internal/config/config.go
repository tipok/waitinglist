package config

import (
	"flag"
	"fmt"
	"time"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	DefaultPort                  = 8080
	DefaultDatabaseURL           = "postgres://localhost:5432/waitinglist?sslmode=disable"
	DefaultEntryBatchSize        = 25
	DefaultEntryWindowInterval   = 30 * time.Hour
	DefaultWaitlistCheckInterval = 1 * time.Hour
)

type Config struct {
	Port              int                     `koanf:"port"`
	Database          DatabaseConfig          `koanf:"database"`
	Waitlist          WaitlistConfig          `koanf:"waitlist"`
	SchedulerInterval SchedulerIntervalConfig `koanf:"schedulerInterval"`
}

type DatabaseConfig struct {
	URL string `koanf:"url"`
}

type WaitlistConfig struct {
	EntryBatchSize      int           `koanf:"entryBatchSize"`
	EntryWindowInterval time.Duration `koanf:"entryWindowInterval"`
}

type SchedulerIntervalConfig struct {
	WaitlistCheckInterval time.Duration `koanf:"waitlistCheckInterval"`
}

// ParseFlags parses the --config flag from os.Args and returns the config file path.
func ParseFlags(args []string) (string, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", "conf/dev.json", "path to JSON configuration file")

	if err := fs.Parse(args); err != nil {
		return "", fmt.Errorf("parsing flags: %w", err)
	}

	if *configPath == "" {
		return "", fmt.Errorf("--config flag is required")
	}

	return *configPath, nil
}

// Load reads the configuration from the given JSON file path and returns a Config
// with defaults applied for any missing fields.
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(file.Provider(path), json.Parser()); err != nil {
		return nil, fmt.Errorf("loading config file: %w", err)
	}

	cfg := &Config{
		Port: DefaultPort,
		Database: DatabaseConfig{
			URL: DefaultDatabaseURL,
		},
		Waitlist: WaitlistConfig{
			EntryBatchSize:      DefaultEntryBatchSize,
			EntryWindowInterval: DefaultEntryWindowInterval,
		},
		SchedulerInterval: SchedulerIntervalConfig{
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
