// 本文件验证多仓库配置默认值、环境变量路径和重复仓库保护。
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadExpandsRepositoryPathsAndDefaults(t *testing.T) {
	t.Setenv("GITCONVERGE_TEST_HOME", filepath.Join(t.TempDir(), "home"))
	configPath := filepath.Join(t.TempDir(), "config.toml")
	content := "version = 1\ninterval = \"10m\"\nrun_timeout = \"8m\"\ncommand_timeout = \"30s\"\nmax_parallel = 2\n\n[[repositories]]\nid = \"notes\"\npath = \"%GITCONVERGE_TEST_HOME%\\\\notes\"\nremote = \"origin\"\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(configuration.Repositories) != 1 {
		t.Fatalf("repositories = %d", len(configuration.Repositories))
	}
	repository := configuration.Repositories[0]
	if repository.Branch != "main" {
		t.Fatalf("branch = %q", repository.Branch)
	}
	if repository.Divergence != "notify" {
		t.Fatalf("divergence = %q", repository.Divergence)
	}
	if repository.CommandTimeout.Duration != 30*time.Second {
		t.Fatalf("timeout = %s", repository.CommandTimeout)
	}
	if repository.FetchInterval.Duration != 10*time.Minute {
		t.Fatalf("fetch interval = %s", repository.FetchInterval)
	}
	if !configuration.Logging.Enabled || configuration.Logging.MaxAge.Duration != 14*24*time.Hour || configuration.Logging.MaxSizeMB != 10 {
		t.Fatalf("logging = %+v", configuration.Logging)
	}
	if !filepath.IsAbs(repository.Path) {
		t.Fatalf("path is not absolute: %s", repository.Path)
	}
}

func TestValidateRejectsInvalidLoggingLimits(t *testing.T) {
	configuration := Config{
		Version: 1, Interval: Duration{10 * time.Minute}, RunTimeout: Duration{8 * time.Minute}, CommandTimeout: Duration{time.Minute}, MaxParallel: 1,
		Logging:      Logging{Enabled: true, MaxAge: Duration{30 * time.Second}, MaxSizeMB: 0},
		Repositories: []Repository{{ID: "one", Path: "C:\\repo", Remote: "origin", Branch: "main", Divergence: "notify", FetchInterval: Duration{10 * time.Minute}}},
	}
	if err := configuration.Validate(); err == nil {
		t.Fatal("expected logging limit error")
	}
}

func TestDefaultLoggingUsesBoundedRetention(t *testing.T) {
	logging := DefaultLogging()
	if !logging.Enabled || logging.MaxAge.Duration != 14*24*time.Hour || logging.MaxBytes() != 10*1024*1024 {
		t.Fatalf("logging = %+v", logging)
	}
}

func TestSaveRoundTripsValidatedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	repository := filepath.Join(t.TempDir(), "notes")
	configuration := Config{
		Logging:      Logging{Enabled: true},
		Notification: Notification{Type: "none"},
		Repositories: []Repository{{ID: "notes", Path: repository, Remote: "origin"}},
	}
	saved, err := Save(path, configuration)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.SourcePath != path || loaded.Repositories[0].Branch != "main" || loaded.Repositories[0].Divergence != "notify" {
		t.Fatalf("saved = %+v, loaded = %+v", saved, loaded)
	}
}

func TestSaveDoesNotReplaceValidConfigWithInvalidInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	valid := Config{Logging: Logging{Enabled: true}, Notification: Notification{Type: "none"}, Repositories: []Repository{{ID: "notes", Path: filepath.Join(t.TempDir(), "notes"), Remote: "origin"}}}
	if _, err := Save(path, valid); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Repositories[0].Remote = ""
	if _, err := Save(path, invalid); err == nil {
		t.Fatal("expected invalid configuration error")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("invalid save replaced the existing configuration")
	}
}

func TestValidateRejectsDuplicatePaths(t *testing.T) {
	configuration := Config{
		Version: 1, Interval: Duration{10 * time.Minute}, RunTimeout: Duration{8 * time.Minute}, CommandTimeout: Duration{time.Minute}, MaxParallel: 1,
		Repositories: []Repository{
			{ID: "one", Path: "C:\\repo", Remote: "origin", Branch: "main", Divergence: "notify"},
			{ID: "two", Path: "c:\\repo", Remote: "origin", Branch: "main", Divergence: "notify"},
		},
	}
	if err := configuration.Validate(); err == nil {
		t.Fatal("expected duplicate path error")
	}
}

func TestValidateRejectsShortFetchInterval(t *testing.T) {
	configuration := Config{
		Version: 1, Interval: Duration{10 * time.Minute}, RunTimeout: Duration{8 * time.Minute}, CommandTimeout: Duration{time.Minute}, MaxParallel: 1,
		Repositories: []Repository{{ID: "one", Path: "C:\\repo", Remote: "origin", Branch: "main", Divergence: "notify", FetchInterval: Duration{30 * time.Second}}},
	}
	if err := configuration.Validate(); err == nil {
		t.Fatal("expected fetch interval error")
	}
}
