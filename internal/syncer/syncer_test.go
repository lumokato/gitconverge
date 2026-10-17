// 本文件使用真实 Git 仓库验证 push、fast-forward、分叉合并和冲突中止。
package syncer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/execx"
	"github.com/lumokato/gitconverge/internal/gitx"
	"github.com/lumokato/gitconverge/internal/state"
)

func TestSyncPushesExistingSemanticCommit(t *testing.T) {
	test := newGitTest(t)
	test.write(test.localA, "notes.txt", "local change\n")
	test.git(test.localA, "add", "notes.txt")
	test.git(test.localA, "commit", "-m", "docs: explain local change")
	result := test.sync(test.localA, "notify")
	if result.Status != StatusSynced || !strings.Contains(result.Action, "push") {
		t.Fatalf("result = %+v", result)
	}
	if test.gitOutput(test.remote, "rev-parse", "refs/heads/main") != test.gitOutput(test.localA, "rev-parse", "HEAD") {
		t.Fatal("remote head did not advance")
	}
}

func TestSyncFastForwardsCleanRepository(t *testing.T) {
	test := newGitTest(t)
	test.write(test.localB, "remote.txt", "remote change\n")
	test.git(test.localB, "add", "remote.txt")
	test.git(test.localB, "commit", "-m", "docs: add remote note")
	test.git(test.localB, "push", "origin", "main")
	result := test.sync(test.localA, "notify")
	if result.Status != StatusSynced || !strings.Contains(result.Action, "fast_forward") {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(test.localA, "remote.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSkipsFetchUntilRepositoryIntervalIsDue(t *testing.T) {
	test := newGitTest(t)
	started := time.Now().Round(time.Second)
	test.write(test.localB, "delayed.txt", "remote change\n")
	test.git(test.localB, "add", "delayed.txt")
	test.git(test.localB, "commit", "-m", "docs: add delayed note")
	test.git(test.localB, "push", "origin", "main")

	result := test.syncAt(test.localA, "notify", state.Repository{LastFetchAt: started}, started.Add(30*time.Minute), time.Hour)
	if result.Status != StatusSynced || result.Action != "already_synced" {
		t.Fatalf("result before interval = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(test.localA, "delayed.txt")); !os.IsNotExist(err) {
		t.Fatalf("remote change arrived before fetch interval: %v", err)
	}

	result = test.syncAt(test.localA, "notify", result.State, started.Add(time.Hour), time.Hour)
	if result.Status != StatusSynced || !strings.Contains(result.Action, "fetch") || !strings.Contains(result.Action, "fast_forward") {
		t.Fatalf("result at interval = %+v", result)
	}
}

func TestSyncFetchesWhenTrackingReferenceIsMissing(t *testing.T) {
	test := newGitTest(t)
	started := time.Now().Round(time.Second)
	test.git(test.localA, "update-ref", "-d", "refs/remotes/origin/main")
	result := test.syncAt(test.localA, "notify", state.Repository{LastFetchAt: started}, started.Add(time.Minute), time.Hour)
	if result.Status != StatusSynced || !strings.Contains(result.Action, "fetch") {
		t.Fatalf("result = %+v", result)
	}
}

func TestSyncFetchesBeforePushEvenWhenIntervalIsNotDue(t *testing.T) {
	test := newGitTest(t)
	started := time.Now().Round(time.Second)
	test.write(test.localA, "local.txt", "local change\n")
	test.git(test.localA, "add", "local.txt")
	test.git(test.localA, "commit", "-m", "docs: add local note")
	result := test.syncAt(test.localA, "notify", state.Repository{LastFetchAt: started}, started.Add(time.Minute), time.Hour)
	if result.Status != StatusSynced || !strings.Contains(result.Action, "fetch") || !strings.Contains(result.Action, "push") {
		t.Fatalf("result = %+v", result)
	}
}
func TestSyncDoesNotRefetchRemoteAheadDirtyRepositoryBeforeInterval(t *testing.T) {
	test := newGitTest(t)
	started := time.Now().Round(time.Second)
	test.write(test.localB, "remote.txt", "remote change\n")
	test.git(test.localB, "add", "remote.txt")
	test.git(test.localB, "commit", "-m", "docs: add remote note")
	test.git(test.localB, "push", "origin", "main")
	test.git(test.localA, "fetch", "origin", "+refs/heads/main:refs/remotes/origin/main")
	test.write(test.localA, "local.txt", "uncommitted\n")
	result := test.syncAt(test.localA, "notify", state.Repository{LastFetchAt: started}, started.Add(time.Minute), time.Hour)
	if result.Status != StatusDeferred || strings.Contains(result.Action, "fetch") {
		t.Fatalf("result = %+v", result)
	}
}

func TestSyncPreservesDirtyFilesAndDefersFastForward(t *testing.T) {
	test := newGitTest(t)
	originalHead := test.gitOutput(test.localA, "rev-parse", "HEAD")
	test.write(test.localA, "shared.txt", "uncommitted local\n")
	test.write(test.localA, "unique.txt", "unique local\n")
	test.write(test.localB, "remote.txt", "remote\n")
	test.git(test.localB, "add", "remote.txt")
	test.git(test.localB, "commit", "-m", "remote update")
	test.git(test.localB, "push", "origin", "main")
	result := test.sync(test.localA, "notify")
	if result.Status != StatusDeferred || !result.Dirty {
		t.Fatalf("result = %+v", result)
	}
	if test.gitOutput(test.localA, "rev-parse", "HEAD") != originalHead {
		t.Fatal("dirty HEAD moved")
	}
	for name, want := range map[string]string{"shared.txt": "uncommitted local\n", "unique.txt": "unique local\n"} {
		got, err := os.ReadFile(filepath.Join(test.localA, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s changed: %q %v", name, got, err)
		}
	}
}

func TestSyncNotifyDivergenceNeverForcePushes(t *testing.T) {
	test := newGitTest(t)
	test.write(test.localA, "shared.txt", "local\n")
	test.git(test.localA, "add", "shared.txt")
	test.git(test.localA, "commit", "-m", "local")
	test.write(test.localB, "shared.txt", "remote\n")
	test.git(test.localB, "add", "shared.txt")
	test.git(test.localB, "commit", "-m", "remote")
	test.git(test.localB, "push", "origin", "main")
	localHead := test.gitOutput(test.localA, "rev-parse", "HEAD")
	remoteHead := test.gitOutput(test.remote, "rev-parse", "main")
	result := test.sync(test.localA, "notify")
	if result.Status != StatusConflict {
		t.Fatalf("result = %+v", result)
	}
	if test.gitOutput(test.localA, "rev-parse", "HEAD") != localHead || test.gitOutput(test.remote, "rev-parse", "main") != remoteHead {
		t.Fatal("diverged history changed")
	}
}

func TestSyncMergesNonConflictingDivergence(t *testing.T) {
	test := newGitTest(t)
	test.write(test.localA, "a.txt", "a\n")
	test.git(test.localA, "add", "a.txt")
	test.git(test.localA, "commit", "-m", "docs: add a")
	test.write(test.localB, "b.txt", "b\n")
	test.git(test.localB, "add", "b.txt")
	test.git(test.localB, "commit", "-m", "docs: add b")
	test.git(test.localB, "push", "origin", "main")
	result := test.sync(test.localA, "merge")
	if result.Status != StatusSynced || !strings.Contains(result.Action, "merge") || !strings.Contains(result.Action, "push") {
		t.Fatalf("result = %+v", result)
	}
}

func TestSyncAbortsConflictingMerge(t *testing.T) {
	test := newGitTest(t)
	test.write(test.localA, "shared.txt", "local\n")
	test.git(test.localA, "add", "shared.txt")
	test.git(test.localA, "commit", "-m", "docs: local version")
	test.write(test.localB, "shared.txt", "remote\n")
	test.git(test.localB, "add", "shared.txt")
	test.git(test.localB, "commit", "-m", "docs: remote version")
	test.git(test.localB, "push", "origin", "main")
	result := test.sync(test.localA, "merge")
	if result.Status != StatusConflict {
		t.Fatalf("result = %+v", result)
	}
	gitDir := test.gitOutput(test.localA, "rev-parse", "--git-dir")
	if _, err := os.Stat(filepath.Join(test.localA, gitDir, "MERGE_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("merge still active: %v", err)
	}
}

func TestSyncDefersWhenRebaseDirectoryExists(t *testing.T) {
	test := newGitTest(t)
	gitDir := test.gitOutput(test.localA, "rev-parse", "--git-dir")
	if err := os.MkdirAll(filepath.Join(test.localA, gitDir, "rebase-merge"), 0o700); err != nil {
		t.Fatal(err)
	}
	result := test.sync(test.localA, "notify")
	if result.Status != StatusDeferred || !strings.Contains(result.Message, "rebase") {
		t.Fatalf("result = %+v", result)
	}
}

func TestFallbackCommitWaitsForCurrentContentToStabilize(t *testing.T) {
	test := newGitTest(t)
	started := time.Now().Round(time.Second)
	repository := config.Repository{
		ID: "test", Path: test.localA, Remote: "origin", Branch: "main", Divergence: "merge",
		FetchInterval: config.Duration{Duration: time.Hour}, CommandTimeout: config.Duration{Duration: 30 * time.Second},
		FallbackCommit: config.FallbackCommit{Enabled: true, After: config.Duration{Duration: 6 * time.Hour}, Message: "recovery(test): preserve unattended changes"},
	}
	worker := Syncer{Git: gitx.Client{Runner: execx.Runner{}, Timeout: 30 * time.Second}}
	test.write(test.localA, "notes.txt", "first draft\n")
	worker.Now = func() time.Time { return started }
	first := worker.Sync(context.Background(), repository, state.Repository{})

	test.write(test.localA, "notes.txt", "second draft\n")
	worker.Now = func() time.Time { return started.Add(6 * time.Hour) }
	second := worker.Sync(context.Background(), repository, first.State)
	if !second.Dirty || !second.State.DirtySince.Equal(started.Add(6*time.Hour)) {
		t.Fatalf("content change did not reset stability window: %+v", second)
	}
	if test.gitOutput(test.localA, "log", "-1", "--pretty=%s") == repository.FallbackCommit.Message {
		t.Fatal("fallback commit was created before current content stabilized")
	}

	worker.Now = func() time.Time { return started.Add(12 * time.Hour) }
	third := worker.Sync(context.Background(), repository, second.State)
	if third.Status != StatusSynced || !strings.Contains(third.Action, "fallback_commit") {
		t.Fatalf("stable fallback commit was not created: %+v", third)
	}
	if test.gitOutput(test.localA, "log", "-1", "--pretty=%s") != repository.FallbackCommit.Message {
		t.Fatal("unexpected fallback commit message")
	}
}

type gitTest struct {
	t                      *testing.T
	remote, localA, localB string
}

func newGitTest(t *testing.T) gitTest {
	t.Helper()
	root := t.TempDir()
	test := gitTest{t: t, remote: filepath.Join(root, "remote.git"), localA: filepath.Join(root, "a"), localB: filepath.Join(root, "b")}
	test.git(root, "init", "--bare", test.remote)
	test.git(root, "clone", test.remote, test.localA)
	test.configure(test.localA)
	test.write(test.localA, "shared.txt", "base\n")
	test.git(test.localA, "add", "shared.txt")
	test.git(test.localA, "commit", "-m", "chore: initialize")
	test.git(test.localA, "branch", "-M", "main")
	test.git(test.localA, "push", "-u", "origin", "main")
	test.git(root, "clone", "--branch", "main", test.remote, test.localB)
	test.configure(test.localB)
	return test
}

func (test gitTest) configure(directory string) {
	test.git(directory, "config", "user.name", "GitConverge Test")
	test.git(directory, "config", "user.email", "test@example.invalid")
}

func (test gitTest) write(directory, name, value string) {
	test.t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(value), 0o600); err != nil {
		test.t.Fatal(err)
	}
}

func (test gitTest) git(directory string, arguments ...string) {
	test.t.Helper()
	_ = test.gitOutput(directory, arguments...)
}
func (test gitTest) gitOutput(directory string, arguments ...string) string {
	test.t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		test.t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func (test gitTest) sync(directory, divergence string) Result {
	test.t.Helper()
	return test.syncAt(directory, divergence, state.Repository{}, time.Now(), 10*time.Minute)
}

func (test gitTest) syncAt(directory, divergence string, previous state.Repository, now time.Time, fetchInterval time.Duration) Result {
	test.t.Helper()
	repository := config.Repository{ID: "test", Path: directory, Remote: "origin", Branch: "main", Divergence: divergence, FetchInterval: config.Duration{Duration: fetchInterval}, CommandTimeout: config.Duration{Duration: 30 * time.Second}}
	worker := Syncer{Git: gitx.Client{Runner: execx.Runner{}, Timeout: 30 * time.Second}, Now: func() time.Time { return now }}
	return worker.Sync(context.Background(), repository, previous)
}
