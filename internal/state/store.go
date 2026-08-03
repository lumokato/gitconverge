// 本文件保存仓库脏状态、通知冷却和最近同步结果，使短命进程之间能够连续判断。
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Repository struct {
	DirtyFingerprint string    `json:"dirtyFingerprint,omitempty"`
	DirtySince       time.Time `json:"dirtySince,omitempty"`
	LastNoticeKey    string    `json:"lastNoticeKey,omitempty"`
	LastNoticeAt     time.Time `json:"lastNoticeAt,omitempty"`
	LastStatus       string    `json:"lastStatus,omitempty"`
	LastAction       string    `json:"lastAction,omitempty"`
	LastMessage      string    `json:"lastMessage,omitempty"`
	LastHead         string    `json:"lastHead,omitempty"`
	LastFetchAt      time.Time `json:"lastFetchAt,omitempty"`
	LastRunAt        time.Time `json:"lastRunAt,omitempty"`
	LastCompletedAt  time.Time `json:"lastCompletedAt,omitempty"`
}

type Store struct {
	Version      int                   `json:"version"`
	Repositories map[string]Repository `json:"repositories"`
	CurrentRun   CurrentRun            `json:"currentRun,omitempty"`
}

type CurrentRun struct {
	StartedAt          time.Time `json:"startedAt,omitempty"`
	Repositories       []string  `json:"repositories,omitempty"`
	ActiveRepositories []string  `json:"activeRepositories,omitempty"`
}

func Load(path string) (Store, error) {
	store := Store{Version: 1, Repositories: map[string]Repository{}}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return Store{}, err
	}
	if err := json.Unmarshal(content, &store); err != nil {
		return Store{}, err
	}
	if store.Version != 1 {
		return Store{}, fmt.Errorf("unsupported state version %d", store.Version)
	}
	if store.Repositories == nil {
		store.Repositories = map[string]Repository{}
	}
	return store, nil
}

func (store Store) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "state-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFile(temporaryPath, path)
}
