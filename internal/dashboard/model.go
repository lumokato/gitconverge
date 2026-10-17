// 本文件把配置、持久状态和 Windows 安装态整理成不含秘密的只读状态快照。
package dashboard

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/runlog"
	"github.com/lumokato/gitconverge/internal/state"
)

type Runtime struct {
	AutomaticPaused    bool     `json:"automaticPaused"`
	SyncActive         bool     `json:"syncActive"`
	ActiveRepositories []string `json:"activeRepositories,omitempty"`
	WorkerInstalled    bool     `json:"workerInstalled"`
	TrayAutostart      bool     `json:"trayAutostart"`
}

type Snapshot struct {
	GeneratedAt  time.Time          `json:"generatedAt"`
	Configured   bool               `json:"configured"`
	ConfigError  string             `json:"configError,omitempty"`
	ConfigPath   string             `json:"configPath"`
	StateDir     string             `json:"stateDir"`
	Summary      Summary            `json:"summary"`
	Settings     Settings           `json:"settings"`
	Runtime      Runtime            `json:"runtime"`
	Repositories []RepositoryStatus `json:"repositories"`
}

type Summary struct {
	Repositories   int        `json:"repositories"`
	Healthy        int        `json:"healthy"`
	NeedsAttention int        `json:"needsAttention"`
	Dirty          int        `json:"dirty"`
	LastCompleted  *time.Time `json:"lastCompleted,omitempty"`
}

type Settings struct {
	Interval               string `json:"interval"`
	RunTimeout             string `json:"runTimeout"`
	CommandTimeout         string `json:"commandTimeout"`
	MaxParallel            int    `json:"maxParallel"`
	LoggingEnabled         bool   `json:"loggingEnabled"`
	LogPath                string `json:"logPath,omitempty"`
	LogMaxAge              string `json:"logMaxAge,omitempty"`
	LogMaxSizeBytes        int64  `json:"logMaxSizeBytes,omitempty"`
	NotificationType       string `json:"notificationType"`
	NotificationConfigured bool   `json:"notificationConfigured"`
}

type RepositoryStatus struct {
	ID              string     `json:"id"`
	Path            string     `json:"path"`
	PathExists      bool       `json:"pathExists"`
	Remote          string     `json:"remote"`
	Branch          string     `json:"branch"`
	Fetch           bool       `json:"fetch"`
	Push            bool       `json:"push"`
	Divergence      string     `json:"divergence"`
	FetchInterval   string     `json:"fetchInterval"`
	FallbackCommit  bool       `json:"fallbackCommit"`
	Dirty           bool       `json:"dirty"`
	DirtySince      *time.Time `json:"dirtySince,omitempty"`
	LastStatus      string     `json:"lastStatus,omitempty"`
	LastAction      string     `json:"lastAction,omitempty"`
	LastMessage     string     `json:"lastMessage,omitempty"`
	LastHead        string     `json:"lastHead,omitempty"`
	LastFetchAt     *time.Time `json:"lastFetchAt,omitempty"`
	LastRunAt       *time.Time `json:"lastRunAt,omitempty"`
	LastCompletedAt *time.Time `json:"lastCompletedAt,omitempty"`
}

func Build(configuration config.Config, store state.Store, runtime Runtime, now time.Time) Snapshot {
	if now.IsZero() {
		now = time.Now()
	}
	if runtime.SyncActive {
		runtime.ActiveRepositories = append([]string(nil), store.CurrentRun.ActiveRepositories...)
	} else {
		runtime.ActiveRepositories = nil
	}
	notificationType := configuration.Notification.Type
	if notificationType == "" {
		notificationType = "none"
	}
	webhook := configuration.Notification.Webhook
	if configuration.Notification.WebhookEnv != "" {
		webhook = os.Getenv(configuration.Notification.WebhookEnv)
	}
	snapshot := Snapshot{
		GeneratedAt:  now,
		Configured:   true,
		ConfigPath:   configuration.SourcePath,
		StateDir:     configuration.StateDir,
		Runtime:      runtime,
		Repositories: []RepositoryStatus{},
		Settings: Settings{
			Interval: configuration.Interval.String(), RunTimeout: configuration.RunTimeout.String(),
			CommandTimeout: configuration.CommandTimeout.String(), MaxParallel: configuration.MaxParallel,
			LoggingEnabled: configuration.Logging.Enabled, LogPath: filepath.Join(configuration.StateDir, runlog.FileName),
			LogMaxAge: configuration.Logging.MaxAge.String(), LogMaxSizeBytes: configuration.Logging.MaxBytes(),
			NotificationType: notificationType, NotificationConfigured: notificationType == "none" || webhook != "",
		},
	}
	var lastCompleted time.Time
	for _, repository := range configuration.Repositories {
		previous := store.Repositories[repository.ID]
		_, pathErr := os.Stat(repository.Path)
		item := RepositoryStatus{
			ID: repository.ID, Path: repository.Path, PathExists: pathErr == nil,
			Remote: repository.Remote, Branch: repository.Branch, Fetch: repository.FetchEnabled(), Push: repository.PushEnabled(),
			Divergence: repository.Divergence, FetchInterval: repository.FetchInterval.String(), FallbackCommit: repository.FallbackCommit.Enabled,
			Dirty: previous.DirtyFingerprint != "", DirtySince: timePointer(previous.DirtySince),
			LastStatus: previous.LastStatus, LastAction: previous.LastAction, LastMessage: previous.LastMessage,
			LastHead: previous.LastHead, LastFetchAt: timePointer(previous.LastFetchAt), LastRunAt: timePointer(previous.LastRunAt), LastCompletedAt: timePointer(previous.LastCompletedAt),
		}
		snapshot.Repositories = append(snapshot.Repositories, item)
		snapshot.Summary.Repositories++
		if item.Dirty {
			snapshot.Summary.Dirty++
		}
		if item.LastStatus == "failed" || item.LastStatus == "conflict" || item.LastStatus == "deferred" || !item.PathExists {
			snapshot.Summary.NeedsAttention++
		} else if item.LastStatus == "synced" {
			snapshot.Summary.Healthy++
		}
		if previous.LastCompletedAt.After(lastCompleted) {
			lastCompleted = previous.LastCompletedAt
		}
	}
	snapshot.Summary.LastCompleted = timePointer(lastCompleted)
	sort.Slice(snapshot.Repositories, func(left, right int) bool { return snapshot.Repositories[left].ID < snapshot.Repositories[right].ID })
	return snapshot
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}
