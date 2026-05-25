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
	"github.com/robfig/cron/v3"

	"github.com/tipok/waitinglist/internal/model"
)

const (
	DefaultPort                  = 8080
	DefaultDatabaseURL           = "postgres://localhost:5432/waitinglist?sslmode=disable"
	DefaultMigrationsDir         = "migrations"
	DefaultEntryBatchSize        = 25
	DefaultEntryWindowInterval   = 30 * time.Hour
	DefaultWaitlistCheckInterval = 1 * time.Hour
	DefaultProjectHeaderName     = "X-Project-ID"
	DefaultProjectSlug           = "default"
)

type Config struct {
	Port              int                     `koanf:"port"`
	Database          DatabaseConfig          `koanf:"database"`
	Waitlist          WaitlistConfig          `koanf:"waitlist"`
	SchedulerInterval SchedulerIntervalConfig `koanf:"schedulerInterval"`
	Admin             AdminConfig             `koanf:"admin"`
	Projects          ProjectsConfig          `koanf:"projects"`
	SMTP              SMTPConfig              `koanf:"smtp"`
}

// SMTPConfig holds global SMTP connection settings for sending notifications.
type SMTPConfig struct {
	Host     string `koanf:"host"`
	Port     int    `koanf:"port"`
	Username string `koanf:"username"`
	Password string `koanf:"password"`
	TLS      bool   `koanf:"tls"`
}

// ProjectsConfig configures multi-tenancy project resolution.
type ProjectsConfig struct {
	HeaderName  string                       `koanf:"headerName"`
	DefaultSlug string                       `koanf:"defaultSlug"`
	Definitions map[string]ProjectDefinition `koanf:"definitions"`
}

// ProjectEmailConfig holds per-project email notification settings in config.
type ProjectEmailConfig struct {
	From    string `koanf:"from"`
	Subject string `koanf:"subject"`
}

// ProjectDigestConfig holds per-project digest email settings in config.
type ProjectDigestConfig struct {
	Recipients []string `koanf:"recipients"`
	Schedule   string   `koanf:"schedule"`
	From       string   `koanf:"from"`
	Subject    string   `koanf:"subject"`
}

// ProjectDefinition holds per-project metadata and optional scheduler overrides.
type ProjectDefinition struct {
	Name                string              `koanf:"name"`
	HostMapping         string              `koanf:"hostMapping"`
	Email               ProjectEmailConfig  `koanf:"email"`
	Digest              ProjectDigestConfig `koanf:"digest"`
	EntryBatchSize      *int                `koanf:"entryBatchSize"`
	EntryWindowInterval string              `koanf:"entryWindowInterval"`
	SchedulerDisabled   bool                `koanf:"schedulerDisabled"`
}

// Validate checks that all slug references in the config are defined in Definitions.
func (p ProjectsConfig) Validate() error {
	if len(p.Definitions) == 0 {
		return fmt.Errorf("projects.definitions must not be empty")
	}
	if _, ok := p.Definitions[p.DefaultSlug]; !ok {
		return fmt.Errorf("projects.defaultSlug %q not found in definitions", p.DefaultSlug)
	}
	seen := make(map[string]string)
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	for slug, def := range p.Definitions {
		if def.HostMapping != "" {
			if other, exists := seen[def.HostMapping]; exists {
				return fmt.Errorf("duplicate hostMapping %q in definitions %q and %q", def.HostMapping, other, slug)
			}
			seen[def.HostMapping] = slug
		}
		if len(def.Digest.Recipients) > 0 && def.Digest.Schedule != "" {
			if _, err := parser.Parse(def.Digest.Schedule); err != nil {
				return fmt.Errorf("projects.definitions[%q].digest.schedule: invalid cron expression %q: %w", slug, def.Digest.Schedule, err)
			}
		}
	}
	return nil
}

// BuildHostMapping derives a host-to-slug map from definitions.
func (p ProjectsConfig) BuildHostMapping() map[string]string {
	m := make(map[string]string)
	for slug, def := range p.Definitions {
		if def.HostMapping != "" {
			m[def.HostMapping] = slug
		}
	}
	return m
}

// Projects returns the definitions as a slice of model.Project.
func (p ProjectsConfig) Projects() []model.Project {
	projects := make([]model.Project, 0, len(p.Definitions))
	for slug, def := range p.Definitions {
		proj := model.Project{
			Slug: slug,
			Name: def.Name,
			Email: model.ProjectEmail{
				From:    def.Email.From,
				Subject: def.Email.Subject,
			},
			Digest: model.ProjectDigest{
				Recipients: def.Digest.Recipients,
				Schedule:   def.Digest.Schedule,
				From:       def.Digest.From,
				Subject:    def.Digest.Subject,
			},
			EntryBatchSize:    def.EntryBatchSize,
			SchedulerDisabled: def.SchedulerDisabled,
		}
		if def.EntryWindowInterval != "" {
			d, _ := time.ParseDuration(def.EntryWindowInterval)
			dur := model.Duration(d)
			proj.EntryWindowInterval = &dur
		}
		projects = append(projects, proj)
	}
	return projects
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
}

// ParseFlags parses CLI arguments and returns the resolved Flags.
func ParseFlags(args []string) (Flags, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", "conf/dev.json", "path to JSON configuration file")
	healthCheck := fs.Bool("health-check", false, "probe /healthz and exit 0/1 (for Docker HEALTHCHECK)")

	if err := fs.Parse(args); err != nil {
		return Flags{}, fmt.Errorf("parsing flags: %w", err)
	}

	if *configPath == "" {
		return Flags{}, fmt.Errorf("--config flag is required")
	}

	return Flags{ConfigPath: *configPath, HealthCheck: *healthCheck}, nil
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
		Projects: ProjectsConfig{
			HeaderName:  DefaultProjectHeaderName,
			DefaultSlug: DefaultProjectSlug,
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
	if cfg.Projects.HeaderName == "" {
		cfg.Projects.HeaderName = DefaultProjectHeaderName
	}
	if cfg.Projects.DefaultSlug == "" {
		cfg.Projects.DefaultSlug = DefaultProjectSlug
	}

	if err := cfg.Projects.Validate(); err != nil {
		return nil, fmt.Errorf("validating projects config: %w", err)
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
