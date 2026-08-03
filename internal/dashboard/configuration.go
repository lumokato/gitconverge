package dashboard

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
)

type ConfigurationDocument struct {
	Configured           bool                 `json:"configured"`
	Interval             string               `json:"interval"`
	RunTimeout           string               `json:"runTimeout"`
	CommandTimeout       string               `json:"commandTimeout"`
	MaxParallel          int                  `json:"maxParallel"`
	NotificationCooldown string               `json:"notificationCooldown"`
	Logging              LoggingDocument      `json:"logging"`
	Notification         NotificationDocument `json:"notification"`
	Repositories         []RepositoryDocument `json:"repositories"`
}

type LoggingDocument struct {
	Enabled   bool   `json:"enabled"`
	MaxAge    string `json:"maxAge"`
	MaxSizeMB int64  `json:"maxSizeMB"`
}

type NotificationDocument struct {
	Type              string `json:"type"`
	Webhook           string `json:"webhook,omitempty"`
	Secret            string `json:"secret,omitempty"`
	WebhookConfigured bool   `json:"webhookConfigured"`
	SecretConfigured  bool   `json:"secretConfigured"`
}

type RepositoryDocument struct {
	ID               string `json:"id"`
	Path             string `json:"path"`
	Remote           string `json:"remote"`
	Branch           string `json:"branch"`
	Fetch            bool   `json:"fetch"`
	Push             bool   `json:"push"`
	Divergence       string `json:"divergence"`
	FetchInterval    string `json:"fetchInterval"`
	DirtyNoticeAfter string `json:"dirtyNoticeAfter"`
	FallbackCommit   bool   `json:"fallbackCommit"`
	FallbackAfter    string `json:"fallbackAfter"`
	FallbackMessage  string `json:"fallbackMessage"`
}

func LoadConfigurationDocument(path string) (ConfigurationDocument, error) {
	configuration, err := config.Load(path)
	configured := true
	if errors.Is(err, os.ErrNotExist) {
		configuration, err = config.Default(path)
		configured = false
	}
	if err != nil {
		return ConfigurationDocument{}, err
	}
	return configurationDocument(configuration, configured), nil
}

func SaveConfigurationDocument(path string, document ConfigurationDocument) (config.Config, error) {
	existing, _ := config.Load(path)
	interval, err := requiredDuration("同步周期", document.Interval)
	if err != nil {
		return config.Config{}, err
	}
	runTimeout, err := requiredDuration("完整运行上限", document.RunTimeout)
	if err != nil {
		return config.Config{}, err
	}
	commandTimeout, err := requiredDuration("单命令上限", document.CommandTimeout)
	if err != nil {
		return config.Config{}, err
	}
	notificationCooldown, err := requiredDuration("通知冷却时间", document.NotificationCooldown)
	if err != nil {
		return config.Config{}, err
	}
	logMaxAge, err := requiredDuration("日志保留时间", document.Logging.MaxAge)
	if err != nil {
		return config.Config{}, err
	}
	configuration := config.Config{
		Version: 1, Interval: config.Duration{Duration: interval}, RunTimeout: config.Duration{Duration: runTimeout},
		CommandTimeout: config.Duration{Duration: commandTimeout}, MaxParallel: document.MaxParallel,
		StateDir: existing.StateDir, NotificationCooldown: config.Duration{Duration: notificationCooldown},
		Logging:      config.Logging{Enabled: document.Logging.Enabled, MaxAge: config.Duration{Duration: logMaxAge}, MaxSizeMB: document.Logging.MaxSizeMB},
		Notification: notificationFromDocument(document.Notification, existing.Notification),
	}
	for index, item := range document.Repositories {
		fetchInterval, err := requiredDuration(fmt.Sprintf("仓库 %d 抓取周期", index+1), item.FetchInterval)
		if err != nil {
			return config.Config{}, err
		}
		dirtyNoticeAfter, err := optionalDuration(fmt.Sprintf("仓库 %d 脏树提醒时间", index+1), item.DirtyNoticeAfter)
		if err != nil {
			return config.Config{}, err
		}
		fallbackAfter, err := optionalDuration(fmt.Sprintf("仓库 %d 保底提交等待时间", index+1), item.FallbackAfter)
		if err != nil {
			return config.Config{}, err
		}
		fetch, push := item.Fetch, item.Push
		configuration.Repositories = append(configuration.Repositories, config.Repository{
			ID: strings.TrimSpace(item.ID), Path: strings.TrimSpace(item.Path), Remote: strings.TrimSpace(item.Remote), Branch: strings.TrimSpace(item.Branch),
			Fetch: &fetch, Push: &push, Divergence: strings.TrimSpace(item.Divergence), FetchInterval: config.Duration{Duration: fetchInterval},
			DirtyNoticeAfter: config.Duration{Duration: dirtyNoticeAfter}, FallbackCommit: config.FallbackCommit{
				Enabled: item.FallbackCommit, After: config.Duration{Duration: fallbackAfter}, Message: strings.TrimSpace(item.FallbackMessage),
			},
		})
	}
	return config.Save(path, configuration)
}

func configurationDocument(configuration config.Config, configured bool) ConfigurationDocument {
	document := ConfigurationDocument{
		Configured: configured, Interval: configuration.Interval.String(), RunTimeout: configuration.RunTimeout.String(),
		CommandTimeout: configuration.CommandTimeout.String(), MaxParallel: configuration.MaxParallel,
		NotificationCooldown: configuration.NotificationCooldown.String(),
		Logging:              LoggingDocument{Enabled: configuration.Logging.Enabled, MaxAge: configuration.Logging.MaxAge.String(), MaxSizeMB: configuration.Logging.MaxSizeMB},
		Notification: NotificationDocument{
			Type:              notificationType(configuration.Notification.Type),
			WebhookConfigured: configuration.Notification.Webhook != "" || configuration.Notification.WebhookEnv != "",
			SecretConfigured:  configuration.Notification.Secret != "" || configuration.Notification.SecretEnv != "",
		},
		Repositories: []RepositoryDocument{},
	}
	for _, repository := range configuration.Repositories {
		document.Repositories = append(document.Repositories, RepositoryDocument{
			ID: repository.ID, Path: repository.Path, Remote: repository.Remote, Branch: repository.Branch,
			Fetch: repository.FetchEnabled(), Push: repository.PushEnabled(), Divergence: repository.Divergence,
			FetchInterval: repository.FetchInterval.String(), DirtyNoticeAfter: durationString(repository.DirtyNoticeAfter),
			FallbackCommit: repository.FallbackCommit.Enabled, FallbackAfter: durationString(repository.FallbackCommit.After), FallbackMessage: repository.FallbackCommit.Message,
		})
	}
	return document
}

func notificationFromDocument(document NotificationDocument, existing config.Notification) config.Notification {
	typeName := notificationType(document.Type)
	if typeName == "none" {
		return config.Notification{Type: "none"}
	}
	notification := existing
	notification.Type = typeName
	if strings.TrimSpace(document.Webhook) != "" {
		notification.Webhook = strings.TrimSpace(document.Webhook)
		notification.WebhookEnv = ""
	}
	if strings.TrimSpace(document.Secret) != "" {
		notification.Secret = strings.TrimSpace(document.Secret)
		notification.SecretEnv = ""
	}
	return notification
}

func notificationType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "none"
	}
	return value
}

func requiredDuration(name, value string) (time.Duration, error) {
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s格式无效: %w", name, err)
	}
	return parsed, nil
}

func optionalDuration(name, value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return requiredDuration(name, value)
}

func durationString(value config.Duration) string {
	if value.Duration == 0 {
		return ""
	}
	return value.String()
}
