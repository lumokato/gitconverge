// 本文件实现单个仓库的一次性同步状态机，不负责长期调度或仓库发现。
package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/execx"
	"github.com/lumokato/gitconverge/internal/gitx"
	"github.com/lumokato/gitconverge/internal/notify"
	"github.com/lumokato/gitconverge/internal/state"
)

type Status string

const (
	StatusSynced   Status = "synced"
	StatusDeferred Status = "deferred"
	StatusConflict Status = "conflict"
	StatusFailed   Status = "failed"
)

type Result struct {
	Repository string           `json:"repository"`
	Status     Status           `json:"status"`
	Action     string           `json:"action"`
	Head       string           `json:"head,omitempty"`
	Dirty      bool             `json:"dirty"`
	Message    string           `json:"message,omitempty"`
	Events     []notify.Event   `json:"-"`
	State      state.Repository `json:"-"`
}

func (result Result) Failed() bool {
	return result.Status == StatusFailed || result.Status == StatusConflict
}

type Syncer struct {
	Git gitx.Client
	Now func() time.Time
}

func (syncer Syncer) Sync(ctx context.Context, repository config.Repository, previous state.Repository) Result {
	now := time.Now()
	if syncer.Now != nil {
		now = syncer.Now()
	}
	result := Result{Repository: repository.ID, Status: StatusSynced, Action: "none", State: previous}
	result.State.LastRunAt = now
	syncer.Git.Timeout = repository.CommandTimeout.Duration

	if _, err := os.Stat(repository.Path); err != nil {
		return syncer.fail(result, "repository_missing", "仓库路径不可用", err.Error(), nil, now)
	}
	inside, err := syncer.Git.Run(ctx, repository.Path, "inspect worktree", "rev-parse", "--is-inside-work-tree")
	if err != nil || inside != "true" {
		if err == nil {
			err = fmt.Errorf("path is not a Git worktree")
		}
		return syncer.fail(result, "not_git_worktree", "路径不是 Git 工作树", err.Error(), nil, now)
	}
	branch, err := syncer.Git.Run(ctx, repository.Path, "inspect branch", "branch", "--show-current")
	if err != nil {
		return syncer.fail(result, "branch_inspection_failed", "无法读取当前分支", err.Error(), nil, now)
	}
	if branch != repository.Branch {
		message := fmt.Sprintf("当前分支是 %q，配置分支是 %q，已跳过自动同步。", branch, repository.Branch)
		return syncer.deferResult(result, "branch_mismatch", "仓库分支不匹配", message, nil, now)
	}
	if operation, active := syncer.activeOperation(ctx, repository.Path); active {
		return syncer.deferResult(result, "git_operation_active", "Git 操作尚未完成", "检测到进行中的 Git 操作："+operation+"。", nil, now)
	}
	if _, err := syncer.Git.Run(ctx, repository.Path, "inspect remote", "remote", "get-url", repository.Remote); err != nil {
		return syncer.fail(result, "remote_missing", "Git 远端不可用", err.Error(), nil, now)
	}

	statusOutput, err := syncer.Git.Run(ctx, repository.Path, "inspect status", "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return syncer.fail(result, "status_failed", "无法读取工作树状态", err.Error(), nil, now)
	}
	dirtyPaths := parseStatusPaths(statusOutput)
	result.Dirty = len(dirtyPaths) > 0
	if result.Dirty {
		currentFingerprint, err := dirtyFingerprint(repository.Path, statusOutput, dirtyPaths)
		if err != nil {
			return syncer.fail(result, "dirty_fingerprint_failed", "无法确认工作树是否稳定", err.Error(), dirtyPaths, now)
		}
		if result.State.DirtyFingerprint != currentFingerprint || result.State.DirtySince.IsZero() {
			result.State.DirtyFingerprint = currentFingerprint
			result.State.DirtySince = now
		}
		dirtyAge := now.Sub(result.State.DirtySince)
		if repository.FallbackCommit.Enabled && dirtyAge >= repository.FallbackCommit.After.Duration {
			if err := syncer.fallbackCommit(ctx, repository); err != nil {
				return syncer.fail(result, "fallback_commit_failed", "保底提交失败", err.Error(), dirtyPaths, now)
			}
			result.Action = "fallback_commit"
			result.Dirty = false
			result.State.DirtyFingerprint = ""
			result.State.DirtySince = time.Time{}
		} else if repository.DirtyNoticeAfter.Duration > 0 && dirtyAge >= repository.DirtyNoticeAfter.Duration {
			result.Events = append(result.Events, notify.Event{
				Repository: repository.ID, Severity: notify.SeverityWarning, Kind: "dirty_worktree",
				Title:   "GitConverge 检测到长期未提交修改",
				Message: fmt.Sprintf("工作树已持续修改 %s。GitConverge 不会替正常任务编写语义提交。", dirtyAge.Round(time.Minute)),
				Paths:   dirtyPaths, At: now,
			})
		}
	} else {
		result.State.DirtyFingerprint = ""
		result.State.DirtySince = time.Time{}
	}

	remoteRef := fmt.Sprintf("refs/remotes/%s/%s", repository.Remote, repository.Branch)
	if syncer.Git.RunAllowFailure(ctx, repository.Path, "rev-parse", "--verify", "--quiet", remoteRef).ExitCode != 0 {
		remoteExists, remoteErr := syncer.remoteBranchExists(ctx, repository)
		if remoteErr != nil {
			return syncer.fail(result, "remote_branch_inspection_failed", "无法检查远端分支", remoteErr.Error(), nil, now)
		}
		if !remoteExists {
			localHead, localErr := syncer.Git.Run(ctx, repository.Path, "read initial local head", "rev-parse", "HEAD")
			if localErr != nil {
				return syncer.fail(result, "local_head_failed", "无法读取本地提交", localErr.Error(), nil, now)
			}
			if !repository.PushEnabled() {
				return syncer.fail(result, "remote_branch_missing", "远端分支不存在", "远端尚无目标分支，且当前仓库已禁用 push。", nil, now)
			}
			if err := syncer.pushAndVerify(ctx, repository); err != nil {
				return syncer.fail(result, "initial_push_failed", "首次 Git push 失败", err.Error(), nil, now)
			}
			result.Action = appendAction(result.Action, "initial_push")
			result.Head = localHead
			result.State.LastHead = localHead
			result.State.LastStatus = string(result.Status)
			return result
		}
	}
	fetchedThisRun := false
	if syncer.fetchDue(ctx, repository, result.State, remoteRef, now) {
		refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", repository.Branch, repository.Remote, repository.Branch)
		if _, err := syncer.Git.Run(ctx, repository.Path, "fetch remote", "fetch", "--prune", repository.Remote, refspec); err != nil {
			return syncer.fail(result, "fetch_failed", "Git fetch 失败", err.Error(), nil, now)
		}
		fetchedThisRun = true
		result.State.LastFetchAt = now
		result.Action = appendAction(result.Action, "fetch")
	}
	localHead, err := syncer.Git.Run(ctx, repository.Path, "read local head", "rev-parse", "HEAD")
	if err != nil {
		return syncer.fail(result, "local_head_failed", "无法读取本地提交", err.Error(), nil, now)
	}
	remoteHead, err := syncer.Git.Run(ctx, repository.Path, "read remote head", "rev-parse", "--verify", remoteRef)
	if err != nil {
		return syncer.fail(result, "remote_head_failed", "无法读取远端提交", err.Error(), nil, now)
	}
	relation, err := syncer.relation(ctx, repository.Path, localHead, remoteHead)
	if err != nil {
		return syncer.fail(result, "relation_failed", "无法判断提交关系", err.Error(), nil, now)
	}
	if relation == "ahead" && repository.FetchEnabled() && !fetchedThisRun {
		refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", repository.Branch, repository.Remote, repository.Branch)
		if _, err := syncer.Git.Run(ctx, repository.Path, "fetch before push", "fetch", "--prune", repository.Remote, refspec); err != nil {
			return syncer.fail(result, "fetch_before_push_failed", "推送前 Git fetch 失败", err.Error(), nil, now)
		}
		result.State.LastFetchAt = now
		result.Action = appendAction(result.Action, "fetch")
		remoteHead, err = syncer.Git.Run(ctx, repository.Path, "refresh remote head", "rev-parse", "--verify", remoteRef)
		if err != nil {
			return syncer.fail(result, "remote_head_failed", "无法读取远端提交", err.Error(), nil, now)
		}
		relation, err = syncer.relation(ctx, repository.Path, localHead, remoteHead)
		if err != nil {
			return syncer.fail(result, "relation_failed", "无法判断提交关系", err.Error(), nil, now)
		}
	}

	switch relation {
	case "equal":
		if result.Action == "none" {
			result.Action = "already_synced"
		}
	case "ahead":
		if repository.PushEnabled() {
			if err := syncer.pushAndVerify(ctx, repository); err != nil {
				return syncer.fail(result, "push_failed", "Git push 失败", err.Error(), nil, now)
			}
			result.Action = appendAction(result.Action, "push")
		} else {
			result.Action = appendAction(result.Action, "local_ahead")
		}
	case "behind":
		if result.Dirty {
			return syncer.deferResult(result, "remote_ahead_dirty", "远端已有更新，但本地存在未提交修改", "为避免覆盖工作树，已延后 fast-forward。", dirtyPaths, now)
		}
		if _, err := syncer.Git.Run(ctx, repository.Path, "fast-forward", "merge", "--ff-only", remoteRef); err != nil {
			return syncer.fail(result, "fast_forward_failed", "Fast-forward 失败", err.Error(), nil, now)
		}
		result.Action = appendAction(result.Action, "fast_forward")
	case "diverged":
		if result.Dirty {
			return syncer.conflict(result, "diverged_dirty", "本地与远端已经分叉，且工作树存在未提交修改", "请先提交或撤销本地修改，再处理分叉提交。", dirtyPaths, now)
		}
		if repository.Divergence != "merge" {
			return syncer.conflict(result, "diverged", "本地与远端提交已经分叉", fmt.Sprintf("本地 %s，远端 %s。配置要求人工处理。", short(localHead), short(remoteHead)), nil, now)
		}
		mergeResult := syncer.Git.RunAllowFailure(ctx, repository.Path, "merge", "--no-edit", remoteRef)
		if !mergeResult.OK() {
			conflictPaths := syncer.conflictPaths(ctx, repository.Path)
			_ = syncer.abortMerge(ctx, repository.Path)
			return syncer.conflict(result, "merge_conflict", "自动合并遇到冲突", commandDetail(mergeResult), conflictPaths, now)
		}
		result.Action = appendAction(result.Action, "merge")
		if repository.PushEnabled() {
			if err := syncer.pushAndVerify(ctx, repository); err != nil {
				return syncer.fail(result, "merge_push_failed", "合并后的 push 失败", err.Error(), nil, now)
			}
			result.Action = appendAction(result.Action, "push")
		}
	}
	finalHead, err := syncer.Git.Run(ctx, repository.Path, "read final head", "rev-parse", "HEAD")
	if err == nil {
		result.Head = finalHead
		result.State.LastHead = finalHead
	}
	result.State.LastStatus = string(result.Status)
	return result
}

func (syncer Syncer) activeOperation(ctx context.Context, directory string) (string, bool) {
	for _, reference := range []struct{ name, ref string }{{"merge", "MERGE_HEAD"}, {"rebase", "REBASE_HEAD"}, {"cherry-pick", "CHERRY_PICK_HEAD"}, {"revert", "REVERT_HEAD"}} {
		if syncer.Git.RunAllowFailure(ctx, directory, "rev-parse", "--verify", "--quiet", reference.ref).ExitCode == 0 {
			return reference.name, true
		}
	}
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		path, err := syncer.Git.Run(ctx, directory, "inspect rebase state", "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		if information, err := os.Stat(path); err == nil && information.IsDir() {
			return "rebase", true
		}
	}
	return "", false
}

func (syncer Syncer) fetchDue(ctx context.Context, repository config.Repository, previous state.Repository, remoteRef string, now time.Time) bool {
	if !repository.FetchEnabled() {
		return false
	}
	if syncer.Git.RunAllowFailure(ctx, repository.Path, "rev-parse", "--verify", "--quiet", remoteRef).ExitCode != 0 {
		return true
	}
	if previous.LastFetchAt.IsZero() || now.Before(previous.LastFetchAt) {
		return true
	}
	return now.Sub(previous.LastFetchAt) >= repository.FetchInterval.Duration
}

func (syncer Syncer) remoteBranchExists(ctx context.Context, repository config.Repository) (bool, error) {
	result := syncer.Git.RunAllowFailure(ctx, repository.Path, "ls-remote", "--exit-code", "--heads", repository.Remote, "refs/heads/"+repository.Branch)
	if result.ExitCode == 0 {
		return strings.TrimSpace(result.Stdout) != "", nil
	}
	if result.ExitCode == 2 && strings.TrimSpace(result.Stdout) == "" {
		return false, nil
	}
	return false, fmt.Errorf("inspect remote branch: %s", commandDetail(result))
}

func (syncer Syncer) fallbackCommit(ctx context.Context, repository config.Repository) error {
	if _, err := syncer.Git.Run(ctx, repository.Path, "stage fallback changes", "add", "-A", "--", "."); err != nil {
		return err
	}
	_, err := syncer.Git.Run(ctx, repository.Path, "create fallback commit", "commit", "-m", repository.FallbackCommit.Message)
	return err
}

func (syncer Syncer) relation(ctx context.Context, directory, localHead, remoteHead string) (string, error) {
	if localHead == remoteHead {
		return "equal", nil
	}
	remoteAncestor := syncer.Git.RunAllowFailure(ctx, directory, "merge-base", "--is-ancestor", remoteHead, localHead)
	if remoteAncestor.ExitCode == 0 {
		return "ahead", nil
	}
	if remoteAncestor.ExitCode != 1 {
		return "", fmt.Errorf("check remote ancestor: %s", commandDetail(remoteAncestor))
	}
	localAncestor := syncer.Git.RunAllowFailure(ctx, directory, "merge-base", "--is-ancestor", localHead, remoteHead)
	if localAncestor.ExitCode == 0 {
		return "behind", nil
	}
	if localAncestor.ExitCode != 1 {
		return "", fmt.Errorf("check local ancestor: %s", commandDetail(localAncestor))
	}
	return "diverged", nil
}

func (syncer Syncer) pushAndVerify(ctx context.Context, repository config.Repository) error {
	if _, err := syncer.Git.Run(ctx, repository.Path, "push", "push", repository.Remote, repository.Branch+":"+repository.Branch); err != nil {
		return err
	}
	localHead, err := syncer.Git.Run(ctx, repository.Path, "read pushed head", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	output, err := syncer.Git.Run(ctx, repository.Path, "verify remote head", "ls-remote", "--heads", repository.Remote, "refs/heads/"+repository.Branch)
	if err != nil {
		return err
	}
	fields := strings.Fields(output)
	if len(fields) < 2 || fields[0] != localHead {
		return fmt.Errorf("remote head verification failed: local %s, remote output %q", localHead, output)
	}
	return nil
}

func (syncer Syncer) conflictPaths(ctx context.Context, directory string) []string {
	output, err := syncer.Git.Run(ctx, directory, "list conflicts", "diff", "--name-only", "--diff-filter=U")
	if err != nil || output == "" {
		return nil
	}
	paths := strings.Split(output, "\n")
	sort.Strings(paths)
	return paths
}

func (syncer Syncer) abortMerge(ctx context.Context, directory string) error {
	result := syncer.Git.RunAllowFailure(ctx, directory, "merge", "--abort")
	if result.ExitCode == 0 {
		return nil
	}
	return fmt.Errorf("merge abort failed: %s", commandDetail(result))
}

func (syncer Syncer) fail(result Result, kind, title, message string, paths []string, now time.Time) Result {
	result.Status = StatusFailed
	result.Message = message
	result.State.LastStatus = string(result.Status)
	result.Events = append(result.Events, notify.Event{Repository: result.Repository, Severity: notify.SeverityError, Kind: kind, Title: title, Message: message, Paths: paths, At: now})
	return result
}

func (syncer Syncer) conflict(result Result, kind, title, message string, paths []string, now time.Time) Result {
	result.Status = StatusConflict
	result.Message = message
	result.State.LastStatus = string(result.Status)
	result.Events = append(result.Events, notify.Event{Repository: result.Repository, Severity: notify.SeverityError, Kind: kind, Title: title, Message: message, Paths: paths, At: now})
	return result
}

func (syncer Syncer) deferResult(result Result, kind, title, message string, paths []string, now time.Time) Result {
	result.Status = StatusDeferred
	result.Message = message
	result.State.LastStatus = string(result.Status)
	result.Events = append(result.Events, notify.Event{Repository: result.Repository, Severity: notify.SeverityWarning, Kind: kind, Title: title, Message: message, Paths: paths, At: now})
	return result
}

func parseStatusPaths(output string) []string {
	if output == "" {
		return nil
	}
	paths := []string{}
	for _, line := range strings.Split(output, "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if arrow := strings.LastIndex(path, " -> "); arrow >= 0 {
			path = path[arrow+4:]
		}
		path = strings.Trim(path, "\"")
		if path != "" {
			paths = append(paths, filepath.ToSlash(path))
		}
	}
	sort.Strings(paths)
	return paths
}

func dirtyFingerprint(root, statusOutput string, paths []string) (string, error) {
	digest := sha256.New()
	_, _ = io.WriteString(digest, statusOutput)
	for _, path := range paths {
		_, _ = io.WriteString(digest, "\x00"+path+"\x00")
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(fullPath)
		if os.IsNotExist(err) {
			_, _ = io.WriteString(digest, "deleted")
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect %s: %w", path, err)
		}
		_, _ = fmt.Fprintf(digest, "%s\x00%d\x00", info.Mode(), info.Size())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(fullPath)
			if err != nil {
				return "", fmt.Errorf("read link %s: %w", path, err)
			}
			_, _ = io.WriteString(digest, target)
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(fullPath)
		if err != nil {
			return "", fmt.Errorf("open %s: %w", path, err)
		}
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", fmt.Errorf("hash %s: %w", path, copyErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close %s: %w", path, closeErr)
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
func appendAction(current, next string) string {
	if current == "" || current == "none" {
		return next
	}
	return current + "+" + next
}
func short(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}
func commandDetail(result execx.Result) string {
	if result.TimedOut {
		return fmt.Sprintf("命令在 %s 后超时。", result.Duration.Round(time.Millisecond))
	}
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" && result.Err != nil {
		detail = result.Err.Error()
	}
	if detail == "" {
		detail = fmt.Sprintf("命令退出码 %d", result.ExitCode)
	}
	return detail
}
