package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return path
}

func TestParseFlags_ValidConfig(t *testing.T) {
	flags, err := ParseFlags([]string{"--config", "/some/path.json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.ConfigPath != "/some/path.json" {
		t.Errorf("expected /some/path.json, got %s", flags.ConfigPath)
	}
	if flags.HealthCheck {
		t.Error("expected HealthCheck to be false")
	}
}

func TestParseFlags_DefaultValue(t *testing.T) {
	flags, err := ParseFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.ConfigPath != "conf/dev.json" {
		t.Errorf("expected default conf/dev.json, got %s", flags.ConfigPath)
	}
}

func TestParseFlags_EmptyValue(t *testing.T) {
	_, err := ParseFlags([]string{"--config", ""})
	if err == nil {
		t.Fatal("expected error for empty --config value")
	}
}

func TestParseFlags_OverrideDefault(t *testing.T) {
	flags, err := ParseFlags([]string{"--config", "custom.json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.ConfigPath != "custom.json" {
		t.Errorf("expected custom.json, got %s", flags.ConfigPath)
	}
}

func TestParseFlags_InvalidFlag(t *testing.T) {
	_, err := ParseFlags([]string{"--unknown"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestParseFlags_HealthCheckFlag(t *testing.T) {
	flags, err := ParseFlags([]string{"--config", "/cfg.json", "--health-check"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.ConfigPath != "/cfg.json" {
		t.Errorf("expected /cfg.json, got %s", flags.ConfigPath)
	}
	if !flags.HealthCheck {
		t.Error("expected HealthCheck to be true")
	}
}

func TestParseFlags_HealthCheckFlagFalseByDefault(t *testing.T) {
	flags, err := ParseFlags([]string{"--config", "/cfg.json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.HealthCheck {
		t.Error("expected HealthCheck to be false when flag is absent")
	}
}

func TestLoad_ValidFullConfig(t *testing.T) {
	path := writeTempConfig(t, `{
		"port": 9090,
		"database": {
			"url": "postgres://myhost:5432/mydb?sslmode=disable",
			"migrationsDir": "/migrations"
		},
		"waitlist": {
			"entryBatchSize": 50,
			"entryWindowInterval": "24h"
		},
		"schedulerInterval": {
			"waitlistCheckInterval": "2h"
		},
		"projects": {
			"definitions": {"default": {"name": "Default"}}
		}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.Database.URL != "postgres://myhost:5432/mydb?sslmode=disable" {
		t.Errorf("unexpected database URL: %s", cfg.Database.URL)
	}
	if cfg.Database.MigrationsDir != "/migrations" {
		t.Errorf("expected migrationsDir /migrations, got %s", cfg.Database.MigrationsDir)
	}
	if cfg.Waitlist.EntryBatchSize != 50 {
		t.Errorf("expected entryBatchSize 50, got %d", cfg.Waitlist.EntryBatchSize)
	}
	if cfg.Waitlist.EntryWindowInterval != 24*time.Hour {
		t.Errorf("expected entryWindowInterval 24h, got %s", cfg.Waitlist.EntryWindowInterval)
	}
	if cfg.SchedulerInterval.WaitlistCheckInterval != 2*time.Hour {
		t.Errorf("expected waitlistCheckInterval 2h, got %s", cfg.SchedulerInterval.WaitlistCheckInterval)
	}
}

func TestLoad_AllFieldsMapped(t *testing.T) {
	path := writeTempConfig(t, `{
		"port": 3000,
		"database": {
			"url": "postgres://testhost:5432/testdb?sslmode=require"
		},
		"waitlist": {
			"entryBatchSize": 10,
			"entryWindowInterval": "48h"
		},
		"schedulerInterval": {
			"waitlistCheckInterval": "30m"
		},
		"projects": {
			"definitions": {"default": {"name": "Default"}}
		}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("expected port 3000, got %d", cfg.Port)
	}
	if cfg.Database.URL != "postgres://testhost:5432/testdb?sslmode=require" {
		t.Errorf("unexpected database URL: %s", cfg.Database.URL)
	}
	if cfg.Waitlist.EntryBatchSize != 10 {
		t.Errorf("expected entryBatchSize 10, got %d", cfg.Waitlist.EntryBatchSize)
	}
	if cfg.Waitlist.EntryWindowInterval != 48*time.Hour {
		t.Errorf("expected entryWindowInterval 48h, got %s", cfg.Waitlist.EntryWindowInterval)
	}
	if cfg.SchedulerInterval.WaitlistCheckInterval != 30*time.Minute {
		t.Errorf("expected waitlistCheckInterval 30m, got %s", cfg.SchedulerInterval.WaitlistCheckInterval)
	}
}

func TestLoad_DefaultsApplied_EmptyObject(t *testing.T) {
	path := writeTempConfig(t, `{"projects":{"definitions":{"default":{"name":"Default"}}}}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("expected default port %d, got %d", DefaultPort, cfg.Port)
	}
	if cfg.Database.URL != DefaultDatabaseURL {
		t.Errorf("expected default database URL %s, got %s", DefaultDatabaseURL, cfg.Database.URL)
	}
	if cfg.Database.MigrationsDir != DefaultMigrationsDir {
		t.Errorf("expected default migrationsDir %s, got %s", DefaultMigrationsDir, cfg.Database.MigrationsDir)
	}
	if cfg.Waitlist.EntryBatchSize != DefaultEntryBatchSize {
		t.Errorf("expected default entryBatchSize %d, got %d", DefaultEntryBatchSize, cfg.Waitlist.EntryBatchSize)
	}
	if cfg.Waitlist.EntryWindowInterval != DefaultEntryWindowInterval {
		t.Errorf("expected default entryWindowInterval %s, got %s", DefaultEntryWindowInterval, cfg.Waitlist.EntryWindowInterval)
	}
	if cfg.SchedulerInterval.WaitlistCheckInterval != DefaultWaitlistCheckInterval {
		t.Errorf("expected default waitlistCheckInterval %s, got %s", DefaultWaitlistCheckInterval, cfg.SchedulerInterval.WaitlistCheckInterval)
	}
}

func TestLoad_PartialConfig_OnlyPort(t *testing.T) {
	path := writeTempConfig(t, `{"port": 4000, "projects":{"definitions":{"default":{"name":"Default"}}}}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 4000 {
		t.Errorf("expected port 4000, got %d", cfg.Port)
	}
	if cfg.Database.URL != DefaultDatabaseURL {
		t.Errorf("expected default database URL, got %s", cfg.Database.URL)
	}
	if cfg.Waitlist.EntryBatchSize != DefaultEntryBatchSize {
		t.Errorf("expected default entryBatchSize, got %d", cfg.Waitlist.EntryBatchSize)
	}
	if cfg.Waitlist.EntryWindowInterval != DefaultEntryWindowInterval {
		t.Errorf("expected default entryWindowInterval, got %s", cfg.Waitlist.EntryWindowInterval)
	}
	if cfg.SchedulerInterval.WaitlistCheckInterval != DefaultWaitlistCheckInterval {
		t.Errorf("expected default waitlistCheckInterval, got %s", cfg.SchedulerInterval.WaitlistCheckInterval)
	}
}

func TestLoad_PartialConfig_OnlyDatabase(t *testing.T) {
	path := writeTempConfig(t, `{"database": {"url": "postgres://other:5432/db"}, "projects":{"definitions":{"default":{"name":"Default"}}}}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("expected default port %d, got %d", DefaultPort, cfg.Port)
	}
	if cfg.Database.URL != "postgres://other:5432/db" {
		t.Errorf("unexpected database URL: %s", cfg.Database.URL)
	}
}

func TestLoad_NonExistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	path := writeTempConfig(t, `{invalid json`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestBuildHostMapping_CollectsFromDefinitions(t *testing.T) {
	pc := ProjectsConfig{
		DefaultSlug: "default",
		Definitions: map[string]ProjectDefinition{
			"default":  {Name: "Default"},
			"beta-app": {Name: "Beta App", HostMapping: "beta.localhost"},
			"tools":    {Name: "Tools", HostMapping: "tools.localhost"},
		},
	}

	m := pc.BuildHostMapping()
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	if m["beta.localhost"] != "beta-app" {
		t.Errorf("expected beta.localhost -> beta-app, got %s", m["beta.localhost"])
	}
	if m["tools.localhost"] != "tools" {
		t.Errorf("expected tools.localhost -> tools, got %s", m["tools.localhost"])
	}
}

func TestBuildHostMapping_SkipsEmpty(t *testing.T) {
	pc := ProjectsConfig{
		DefaultSlug: "default",
		Definitions: map[string]ProjectDefinition{
			"default":  {Name: "Default"},
			"beta-app": {Name: "Beta App", HostMapping: "beta.localhost"},
		},
	}

	m := pc.BuildHostMapping()
	if len(m) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(m))
	}
	if m["beta.localhost"] != "beta-app" {
		t.Errorf("expected beta.localhost -> beta-app, got %s", m["beta.localhost"])
	}
}

func TestValidate_DuplicateHostMapping_ReturnsError(t *testing.T) {
	pc := ProjectsConfig{
		DefaultSlug: "default",
		Definitions: map[string]ProjectDefinition{
			"default": {Name: "Default"},
			"app-a":   {Name: "App A", HostMapping: "same.host"},
			"app-b":   {Name: "App B", HostMapping: "same.host"},
		},
	}

	err := pc.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate hostMapping")
	}
}

func TestValidate_NoDefinitions_ReturnsError(t *testing.T) {
	pc := ProjectsConfig{
		DefaultSlug: "default",
		Definitions: map[string]ProjectDefinition{},
	}

	err := pc.Validate()
	if err == nil {
		t.Fatal("expected error for empty definitions")
	}
}

func TestValidate_DefaultSlugMissing_ReturnsError(t *testing.T) {
	pc := ProjectsConfig{
		DefaultSlug: "missing",
		Definitions: map[string]ProjectDefinition{
			"default": {Name: "Default"},
		},
	}

	err := pc.Validate()
	if err == nil {
		t.Fatal("expected error for missing defaultSlug in definitions")
	}
}

func TestLoad_WithHostMappingInDefinitions(t *testing.T) {
	path := writeTempConfig(t, `{
		"projects": {
			"defaultSlug": "default",
			"definitions": {
				"default": {"name": "Default"},
				"beta": {"name": "Beta", "hostMapping": "beta.example.com", "entryBatchSize": 5}
			}
		}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := cfg.Projects.BuildHostMapping()
	if m["beta.example.com"] != "beta" {
		t.Errorf("expected beta.example.com -> beta, got %s", m["beta.example.com"])
	}
}
