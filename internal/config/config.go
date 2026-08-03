// 本文件负责加载通用多仓库配置、应用默认值并解析跨机器路径。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Duration struct {
	time.Duration
}

func (duration *Duration) UnmarshalText(value []byte) error {
	parsed, err := time.ParseDuration(string(value))
	if err != nil {
		return err
	}
	duration.Duration = parsed
	return nil
}

func (duration Duration) MarshalText() ([]byte, error) {
	return []byte(duration.String()), nil
}

type Config struct {
	Version              int          `toml:"version"`
	Interval             Duration     `toml:"interval"`
	RunTimeout           Duration     `toml:"run_timeout"`
	CommandTimeout       Duration     `toml:"command_timeout"`
	MaxParallel          int          `toml:"max_parallel"`
	StateDir             string       `toml:"state_dir"`
	NotificationCooldown Duration     `toml:"notification_cooldown"`
	Logging              Logging      `toml:"logging"`
	Notification         Notification `toml:"notification"`
	Repositories         []Repository `toml:"repositories"`
	SourcePath           string       `toml:"-"`
}

type Logging struct {
	Enabled   bool     `toml:"enabled"`
	MaxAge    Duration `toml:"max_age"`
	MaxSizeMB int64    `toml:"max_size_mb"`
}

func (logging Logging) MaxBytes() int64 {
	return logging.MaxSizeMB * 1024 * 1024
}

type Notification struct {
	Type       string `toml:"type"`
	Webhook    string `toml:"webhook"`
	WebhookEnv string `toml:"webhook_env"`
	Secret     string `toml:"secret"`
	SecretEnv  string `toml:"secret_env"`
}

type Repository struct {
	ID               string         `toml:"id"`
	Path             string         `toml:"path"`
	Remote           string         `toml:"remote"`
	Branch           string         `toml:"branch"`
	Fetch            *bool          `toml:"fetch"`
	Push             *bool          `toml:"push"`
	Divergence       string         `toml:"divergence"`
	FetchInterval    Duration       `toml:"fetch_interval"`
	CommandTimeout   Duration       `toml:"command_timeout"`
	DirtyNoticeAfter Duration       `toml:"dirty_notice_after"`
	FallbackCommit   FallbackCommit `toml:"fallback_commit"`
}

type FallbackCommit struct {
	Enabled bool     `toml:"enabled"`
	After   Duration `toml:"after"`
	Message string   `toml:"message"`
}

func (repository Repository) FetchEnabled() bool {
	return repository.Fetch == nil || *repository.Fetch
}

func (repository Repository) PushEnabled() bool {
	return repository.Push == nil || *repository.Push
}

func DefaultPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "GitConverge", "config.toml"), nil
}

func DefaultStateDir() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "GitConverge"), nil
}

func DefaultLogging() Logging {
	return Logging{
		Enabled:   true,
		MaxAge:    Duration{14 * 24 * time.Hour},
		MaxSizeMB: 10,
	}
}

func Default(path string) (Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, err
	}
	stateDir, err := DefaultStateDir()
	if err != nil {
		return Config{}, err
	}
	return Config{
		Version:              1,
		Interval:             Duration{10 * time.Minute},
		RunTimeout:           Duration{8 * time.Minute},
		CommandTimeout:       Duration{3 * time.Minute},
		MaxParallel:          2,
		StateDir:             stateDir,
		NotificationCooldown: Duration{6 * time.Hour},
		Logging:              DefaultLogging(),
		SourcePath:           absolutePath,
	}, nil
}

func Load(path string) (Config, error) {
	configuration, err := Default(path)
	if err != nil {
		return Config{}, err
	}
	content, err := os.ReadFile(configuration.SourcePath)
	if err != nil {
		return Config{}, err
	}
	if err := toml.Unmarshal(content, &configuration); err != nil {
		return Config{}, err
	}
	return Normalize(configuration, configuration.SourcePath)
}

func Normalize(configuration Config, path string) (Config, error) {
	defaults, err := Default(path)
	if err != nil {
		return Config{}, err
	}
	applyDefaults(&configuration, defaults)
	configuration.SourcePath = defaults.SourcePath
	baseDir := filepath.Dir(configuration.SourcePath)
	configuration.StateDir, err = expandPath(configuration.StateDir, baseDir)
	if err != nil {
		return Config{}, fmt.Errorf("expand state_dir: %w", err)
	}
	for index := range configuration.Repositories {
		repository := &configuration.Repositories[index]
		repository.Path, err = expandPath(repository.Path, baseDir)
		if err != nil {
			return Config{}, fmt.Errorf("expand repository %q path: %w", repository.ID, err)
		}
		if repository.Branch == "" {
			repository.Branch = "main"
		}
		if repository.Divergence == "" {
			repository.Divergence = "notify"
		}
		if repository.FetchInterval.Duration == 0 {
			repository.FetchInterval = configuration.Interval
		}
		if repository.CommandTimeout.Duration == 0 {
			repository.CommandTimeout = configuration.CommandTimeout
		}
		if repository.FallbackCommit.Enabled && repository.FallbackCommit.After.Duration == 0 {
			repository.FallbackCommit.After = Duration{6 * time.Hour}
		}
		if repository.FallbackCommit.Enabled && repository.FallbackCommit.Message == "" {
			repository.FallbackCommit.Message = fmt.Sprintf("recovery(%s): preserve unattended changes", repository.ID)
		}
	}
	if err := configuration.Validate(); err != nil {
		return Config{}, err
	}
	return configuration, nil
}

func applyDefaults(configuration *Config, defaults Config) {
	if configuration.Version == 0 {
		configuration.Version = defaults.Version
	}
	if configuration.Interval.Duration == 0 {
		configuration.Interval = defaults.Interval
	}
	if configuration.RunTimeout.Duration == 0 {
		configuration.RunTimeout = defaults.RunTimeout
	}
	if configuration.CommandTimeout.Duration == 0 {
		configuration.CommandTimeout = defaults.CommandTimeout
	}
	if configuration.MaxParallel == 0 {
		configuration.MaxParallel = defaults.MaxParallel
	}
	if configuration.StateDir == "" {
		configuration.StateDir = defaults.StateDir
	}
	if configuration.NotificationCooldown.Duration == 0 {
		configuration.NotificationCooldown = defaults.NotificationCooldown
	}
	if configuration.Logging.MaxAge.Duration == 0 {
		configuration.Logging.MaxAge = defaults.Logging.MaxAge
	}
	if configuration.Logging.MaxSizeMB == 0 {
		configuration.Logging.MaxSizeMB = defaults.Logging.MaxSizeMB
	}
}

func (configuration Config) Validate() error {
	if configuration.Version != 1 {
		return fmt.Errorf("unsupported config version %d", configuration.Version)
	}
	if configuration.Interval.Duration < time.Minute {
		return errors.New("interval must be at least one minute")
	}
	if configuration.RunTimeout.Duration < time.Minute {
		return errors.New("run_timeout must be at least one minute")
	}
	if configuration.CommandTimeout.Duration <= 0 {
		return errors.New("command_timeout must be positive")
	}
	if configuration.MaxParallel < 1 || configuration.MaxParallel > 16 {
		return errors.New("max_parallel must be between 1 and 16")
	}
	if configuration.Logging.Enabled {
		if configuration.Logging.MaxAge.Duration < time.Minute {
			return errors.New("logging.max_age must be at least one minute")
		}
		if configuration.Logging.MaxSizeMB < 1 || configuration.Logging.MaxSizeMB > 1024 {
			return errors.New("logging.max_size_mb must be between 1 and 1024")
		}
	}
	if len(configuration.Repositories) == 0 {
		return errors.New("at least one repository is required")
	}
	if configuration.Notification.Type != "" && configuration.Notification.Type != "none" && configuration.Notification.Type != "dingtalk" {
		return fmt.Errorf("unsupported notification type %q", configuration.Notification.Type)
	}
	if configuration.Notification.Type == "dingtalk" && configuration.Notification.Webhook == "" && configuration.Notification.WebhookEnv == "" {
		return errors.New("dingtalk notification requires webhook or webhook_env")
	}
	identifiers := map[string]struct{}{}
	paths := map[string]struct{}{}
	for _, repository := range configuration.Repositories {
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(repository.ID) {
			return fmt.Errorf("repository id %q is invalid", repository.ID)
		}
		if _, exists := identifiers[strings.ToLower(repository.ID)]; exists {
			return fmt.Errorf("repository id %q is duplicated", repository.ID)
		}
		identifiers[strings.ToLower(repository.ID)] = struct{}{}
		normalizedPath := strings.ToLower(filepath.Clean(repository.Path))
		if _, exists := paths[normalizedPath]; exists {
			return fmt.Errorf("repository path %q is duplicated", repository.Path)
		}
		paths[normalizedPath] = struct{}{}
		if repository.Remote == "" {
			return fmt.Errorf("repository %q remote is required", repository.ID)
		}
		if repository.Branch == "" {
			return fmt.Errorf("repository %q branch is required", repository.ID)
		}
		if repository.Divergence != "notify" && repository.Divergence != "merge" {
			return fmt.Errorf("repository %q divergence must be notify or merge", repository.ID)
		}
		if repository.FetchEnabled() && repository.FetchInterval.Duration < time.Minute {
			return fmt.Errorf("repository %q fetch interval must be at least one minute", repository.ID)
		}
		if repository.FallbackCommit.Enabled && repository.FallbackCommit.After.Duration < time.Minute {
			return fmt.Errorf("repository %q fallback commit delay must be at least one minute", repository.ID)
		}
	}
	return nil
}

var percentEnvironment = regexp.MustCompile(`%([^%]+)%`)

func expandPath(value string, baseDir string) (string, error) {
	value = percentEnvironment.ReplaceAllStringFunc(value, func(match string) string {
		name := strings.Trim(match, "%")
		if replacement, exists := os.LookupEnv(name); exists {
			return replacement
		}
		return match
	})
	value = os.ExpandEnv(value)
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimLeft(value[1:], `/\`))
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(baseDir, value)
	}
	return filepath.Abs(value)
}
