// 本文件验证状态快照不暴露通知凭据，并正确汇总仓库状态。
package dashboard

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/state"
)

func TestBuildSummarizesRepositoriesWithoutSecrets(t *testing.T) {
	now := time.Date(2026, time.August, 2, 14, 0, 0, 0, time.UTC)
	path := t.TempDir()
	configuration := config.Config{
		SourcePath: filepath.Join(path, "config.toml"), StateDir: filepath.Join(path, "state"),
		Interval: config.Duration{Duration: 10 * time.Minute}, RunTimeout: config.Duration{Duration: 8 * time.Minute},
		CommandTimeout: config.Duration{Duration: 3 * time.Minute}, MaxParallel: 2,
		Logging:      config.Logging{Enabled: true, MaxAge: config.Duration{Duration: 14 * 24 * time.Hour}, MaxSizeMB: 10},
		Notification: config.Notification{Type: "dingtalk", Webhook: "https://secret.invalid/hook", Secret: "secret"},
		Repositories: []config.Repository{{ID: "notes", Path: path, Remote: "origin", Branch: "main", Divergence: "notify", FetchInterval: config.Duration{Duration: 10 * time.Minute}}},
	}
	store := state.Store{Version: 1, Repositories: map[string]state.Repository{"notes": {LastStatus: "synced", LastAction: "push", LastCompletedAt: now}}}
	snapshot := Build(configuration, store, Runtime{WorkerInstalled: true, TrayAutostart: true}, now)
	if snapshot.Summary.Repositories != 1 || snapshot.Summary.Healthy != 1 || snapshot.Summary.NeedsAttention != 0 {
		t.Fatalf("summary = %+v", snapshot.Summary)
	}
	if !snapshot.Settings.NotificationConfigured || snapshot.Settings.NotificationType != "dingtalk" {
		t.Fatalf("notification = %+v", snapshot.Settings)
	}
	if !snapshot.Settings.LoggingEnabled || snapshot.Settings.LogMaxSizeBytes != 10*1024*1024 || filepath.Base(snapshot.Settings.LogPath) != "runs.jsonl" {
		t.Fatalf("logging = %+v", snapshot.Settings)
	}
	if snapshot.Repositories[0].LastAction != "push" || !snapshot.Repositories[0].PathExists {
		t.Fatalf("repository = %+v", snapshot.Repositories[0])
	}
}

func TestBuildOmitsZeroCompletionTimes(t *testing.T) {
	path := t.TempDir()
	configuration := config.Config{
		SourcePath: filepath.Join(path, "config.toml"), StateDir: filepath.Join(path, "state"),
		Repositories: []config.Repository{{ID: "notes", Path: path, Remote: "origin", Branch: "main"}},
	}
	snapshot := Build(configuration, state.Store{Version: 1, Repositories: map[string]state.Repository{}}, Runtime{}, time.Now())
	if snapshot.Summary.LastCompleted != nil || snapshot.Repositories[0].LastCompletedAt != nil {
		t.Fatalf("zero completion times were retained: %+v", snapshot)
	}
}
