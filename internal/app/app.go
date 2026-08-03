// 本文件协调多仓库并发、状态持久化、通知冷却和命令行输出。
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/gitx"
	"github.com/lumokato/gitconverge/internal/lock"
	"github.com/lumokato/gitconverge/internal/notify"
	"github.com/lumokato/gitconverge/internal/state"
	"github.com/lumokato/gitconverge/internal/syncer"
)

type Application struct {
	Git      gitx.Client
	Notifier notify.Notifier
	Now      func() time.Time
}

func (application Application) Run(ctx context.Context, configuration config.Config) ([]syncer.Result, error) {
	instanceLock, acquired, err := lock.Acquire("GitConverge")
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, fmt.Errorf("another GitConverge run is active")
	}
	defer instanceLock.Release()

	statePath := filepath.Join(configuration.StateDir, "state.json")
	store, err := state.Load(statePath)
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	now := time.Now()
	if application.Now != nil {
		now = application.Now()
	}
	repositoryIDs := make([]string, 0, len(configuration.Repositories))
	for _, repository := range configuration.Repositories {
		repositoryIDs = append(repositoryIDs, repository.ID)
	}
	store.CurrentRun = state.CurrentRun{StartedAt: now, Repositories: repositoryIDs}
	if err := store.Save(statePath); err != nil {
		return nil, fmt.Errorf("save active run state: %w", err)
	}

	type indexedResult struct {
		Index  int
		Result syncer.Result
	}
	resultsChannel := make(chan indexedResult, len(configuration.Repositories))
	semaphore := make(chan struct{}, configuration.MaxParallel)
	var stateMutex sync.Mutex
	setRepositoryActive := func(repositoryID string, active bool) {
		stateMutex.Lock()
		defer stateMutex.Unlock()
		filtered := store.CurrentRun.ActiveRepositories[:0]
		for _, currentID := range store.CurrentRun.ActiveRepositories {
			if currentID != repositoryID {
				filtered = append(filtered, currentID)
			}
		}
		store.CurrentRun.ActiveRepositories = filtered
		if active {
			store.CurrentRun.ActiveRepositories = append(store.CurrentRun.ActiveRepositories, repositoryID)
			sort.Strings(store.CurrentRun.ActiveRepositories)
		}
		if err := store.Save(statePath); err != nil {
			fmt.Fprintf(os.Stderr, "save active repository state: %v\n", err)
		}
	}
	var waitGroup sync.WaitGroup
	for index, repository := range configuration.Repositories {
		index, repository := index, repository
		previous := store.Repositories[repository.ID]
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				resultsChannel <- indexedResult{Index: index, Result: syncer.Result{Repository: repository.ID, Status: syncer.StatusFailed, Message: ctx.Err().Error(), State: previous}}
				return
			}
			setRepositoryActive(repository.ID, true)
			defer setRepositoryActive(repository.ID, false)
			worker := syncer.Syncer{Git: application.Git, Now: application.Now}
			resultsChannel <- indexedResult{Index: index, Result: worker.Sync(ctx, repository, previous)}
		}()
	}
	waitGroup.Wait()
	close(resultsChannel)

	indexed := make([]indexedResult, 0, len(configuration.Repositories))
	for result := range resultsChannel {
		indexed = append(indexed, result)
	}
	sort.Slice(indexed, func(left, right int) bool { return indexed[left].Index < indexed[right].Index })
	notifier := application.Notifier
	if notifier == nil {
		notifier = notify.Noop{}
	}
	results := make([]syncer.Result, 0, len(indexed))
	for _, item := range indexed {
		result := item.Result
		repositoryState := result.State
		for _, event := range result.Events {
			eventKey := event.Key()
			if repositoryState.LastNoticeKey == eventKey && now.Sub(repositoryState.LastNoticeAt) < configuration.NotificationCooldown.Duration {
				continue
			}
			notifyContext, cancel := context.WithTimeout(ctx, 20*time.Second)
			notifyErr := notifier.Notify(notifyContext, event)
			cancel()
			if notifyErr != nil {
				fmt.Fprintf(os.Stderr, "notification for %s failed: %v\n", result.Repository, notifyErr)
				continue
			}
			repositoryState.LastNoticeKey = eventKey
			repositoryState.LastNoticeAt = now
		}
		repositoryState.LastStatus = string(result.Status)
		repositoryState.LastAction = result.Action
		repositoryState.LastMessage = result.Message
		repositoryState.LastCompletedAt = now
		result.State = repositoryState
		store.Repositories[result.Repository] = repositoryState
		results = append(results, result)
	}
	store.CurrentRun = state.CurrentRun{}
	if err := store.Save(statePath); err != nil {
		return results, fmt.Errorf("save state: %w", err)
	}
	return results, nil
}

func WriteResults(results []syncer.Result, output *os.File, asJSON bool) error {
	if output == nil {
		output = os.Stdout
	}
	if asJSON {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}
	for _, result := range results {
		head := result.Head
		if len(head) > 8 {
			head = head[:8]
		}
		fmt.Fprintf(output, "%-20s %-9s %-24s %s", result.Repository, result.Status, result.Action, head)
		if result.Message != "" {
			fmt.Fprintf(output, "  %s", result.Message)
		}
		fmt.Fprintln(output)
	}
	return nil
}
