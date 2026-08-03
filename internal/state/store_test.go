// 本文件验证状态文件可重复覆盖，并保持短命进程之间的关键时间戳。
package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveReplacesExistingStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	firstFetch := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	store := Store{Version: 1, Repositories: map[string]Repository{"notes": {LastStatus: "synced", LastAction: "push", LastFetchAt: firstFetch}}, CurrentRun: CurrentRun{StartedAt: firstFetch, Repositories: []string{"notes"}, ActiveRepositories: []string{"notes"}}}
	if err := store.Save(path); err != nil {
		t.Fatal(err)
	}
	secondFetch := firstFetch.Add(10 * time.Minute)
	store.Repositories["notes"] = Repository{LastStatus: "failed", LastAction: "fetch", LastMessage: "network unavailable", LastFetchAt: secondFetch, LastCompletedAt: secondFetch}
	if err := store.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Repositories["notes"]
	if got.LastStatus != "failed" || got.LastAction != "fetch" || got.LastMessage != "network unavailable" || !got.LastFetchAt.Equal(secondFetch) || !got.LastCompletedAt.Equal(secondFetch) {
		t.Fatalf("state = %+v", got)
	}
	if len(loaded.CurrentRun.Repositories) != 1 || loaded.CurrentRun.Repositories[0] != "notes" {
		t.Fatalf("current run = %+v", loaded.CurrentRun)
	}
	if len(loaded.CurrentRun.ActiveRepositories) != 1 || loaded.CurrentRun.ActiveRepositories[0] != "notes" {
		t.Fatalf("active repositories = %+v", loaded.CurrentRun.ActiveRepositories)
	}
}
