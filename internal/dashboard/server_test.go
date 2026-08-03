// 本文件验证状态页鉴权、JSON 输出和秘密隔离。
package dashboard

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lumokato/gitconverge/internal/runlog"
	"github.com/lumokato/gitconverge/internal/state"
)

func TestStatusAPIRequiresTokenAndOmitsSecrets(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "repo")
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	configPath := filepath.Join(root, "config.toml")
	content := "version = 1\ninterval = \"10m\"\nrun_timeout = \"8m\"\ncommand_timeout = \"3m\"\nmax_parallel = 1\nstate_dir = \"" + strings.ReplaceAll(stateDir, "\\", "\\\\") + "\"\n\n[notification]\ntype = \"dingtalk\"\nwebhook = \"https://secret.invalid/hook\"\nsecret = \"hidden-secret\"\n\n[[repositories]]\nid = \"notes\"\npath = \"" + strings.ReplaceAll(repository, "\\", "\\\\") + "\"\nremote = \"origin\"\nbranch = \"main\"\ndivergence = \"notify\"\nfetch_interval = \"10m\"\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.Store{Version: 1, Repositories: map[string]state.Repository{"notes": {LastStatus: "synced", LastAction: "push", LastCompletedAt: time.Now()}}}
	if err := store.Save(filepath.Join(stateDir, "state.json")); err != nil {
		t.Fatal(err)
	}
	server, err := Start(configPath, filepath.Join(root, "worker.exe"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })

	response, err := http.Get(strings.Split(server.URL, "?")[0] + "api/status")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status without token = %d", response.StatusCode)
	}

	statusURL := strings.Replace(server.URL, "/?token=", "/api/status?token=", 1)
	response, err = http.Get(statusURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var snapshot Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "secret.invalid") || strings.Contains(string(encoded), "hidden-secret") {
		t.Fatalf("status leaked secret: %s", encoded)
	}
	if snapshot.Summary.Repositories != 1 || snapshot.Repositories[0].ID != "notes" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if !snapshot.Configured {
		t.Fatal("configured snapshot was marked incomplete")
	}
}

func TestServerStaysAvailableWhenConfigIsMissing(t *testing.T) {
	server, err := Start(filepath.Join(t.TempDir(), "missing.toml"), filepath.Join(t.TempDir(), "worker.exe"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })

	statusURL := strings.Replace(server.URL, "/?token=", "/api/status?token=", 1)
	response, err := http.Get(statusURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status with missing config = %d", response.StatusCode)
	}
	var snapshot Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Configured || snapshot.Summary.Repositories != 0 {
		t.Fatalf("first-run snapshot = %+v", snapshot)
	}
}

func TestDashboardSaveHandlerDoesNotShadowDocument(t *testing.T) {
	for _, forbidden := range []string{"var document=collectConfiguration()", "function renderLogs(document)"} {
		if strings.Contains(indexHTML, forbidden) {
			t.Fatalf("dashboard shadows the browser document object with %q", forbidden)
		}
	}
	for _, expected := range []string{"/api/config", "/api/logs", "saveConfiguration", "选择文件夹"} {
		if !strings.Contains(indexHTML, expected) {
			t.Fatalf("dashboard is missing %q", expected)
		}
	}
}

func TestConfigAPIWritesValidatedConfigurationWithoutReturningSecrets(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	repository := filepath.Join(root, "repo")
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	server, err := Start(configPath, filepath.Join(root, "worker.exe"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })
	if server.managed {
		t.Fatal("temporary dashboard was treated as a managed installation")
	}

	document := ConfigurationDocument{
		Interval: "10m", RunTimeout: "8m", CommandTimeout: "3m", MaxParallel: 2, NotificationCooldown: "6h",
		Logging:      LoggingDocument{Enabled: true, MaxAge: "336h", MaxSizeMB: 10},
		Notification: NotificationDocument{Type: "dingtalk", Webhook: "https://secret.invalid/hook", Secret: "hidden-secret"},
		Repositories: []RepositoryDocument{{ID: "notes", Path: repository, Remote: "origin", Branch: "main", Fetch: true, Push: true, Divergence: "notify", FetchInterval: "10m", DirtyNoticeAfter: "30m"}},
	}
	payload, _ := json.Marshal(document)
	request, err := http.NewRequest(http.MethodPut, strings.Replace(server.URL, "/?token=", "/api/config?token=", 1), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("save status = %d: %s", response.StatusCode, body)
	}

	configURL := strings.Replace(server.URL, "/?token=", "/api/config?token=", 1)
	response, err = http.Get(configURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var loaded ConfigurationDocument
	if err := json.NewDecoder(response.Body).Decode(&loaded); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(loaded)
	if strings.Contains(string(encoded), "secret.invalid") || strings.Contains(string(encoded), "hidden-secret") {
		t.Fatalf("config API returned secret values: %s", encoded)
	}
	if !loaded.Configured || !loaded.Notification.WebhookConfigured || !loaded.Notification.SecretConfigured || len(loaded.Repositories) != 1 {
		t.Fatalf("loaded config = %+v", loaded)
	}
}

func TestLogsAPIReturnsNewestEntriesFirst(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	configPath := filepath.Join(root, "config.toml")
	repository := filepath.Join(root, "repo")
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "version = 1\nstate_dir = \"" + strings.ReplaceAll(stateDir, "\\", "\\\\") + "\"\n\n[[repositories]]\nid = \"notes\"\npath = \"" + strings.ReplaceAll(repository, "\\", "\\\\") + "\"\nremote = \"origin\"\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Minute)
	for index, status := range []string{"failed", "synced"} {
		at := started.Add(time.Duration(index) * time.Second)
		if err := runlog.Append(filepath.Join(stateDir, runlog.FileName), runlog.Entry{StartedAt: at, CompletedAt: at.Add(time.Second), Status: status}, runlog.Policy{MaxAge: 24 * time.Hour, MaxBytes: 1024 * 1024}); err != nil {
			t.Fatal(err)
		}
	}
	server, err := Start(configPath, filepath.Join(root, "worker.exe"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })
	logsURL := strings.Replace(server.URL, "/?token=", "/api/logs?limit=10&token=", 1)
	response, err := http.Get(logsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var document LogDocument
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	if len(document.Entries) != 2 || document.Entries[0].Status != "synced" {
		t.Fatalf("logs = %+v", document.Entries)
	}
}
