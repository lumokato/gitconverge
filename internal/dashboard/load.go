// 本文件从配置、状态文件和单实例锁构建当前状态快照。
package dashboard

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/lock"
	"github.com/lumokato/gitconverge/internal/state"
)

func Load(configPath string, runtime Runtime) (Snapshot, error) {
	configuration, err := config.Load(configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, err
		}
		configuration, err = config.Default(configPath)
		if err != nil {
			return Snapshot{}, err
		}
		snapshot := Build(configuration, state.Store{Version: 1, Repositories: map[string]state.Repository{}}, runtime, time.Now())
		snapshot.Configured = false
		return snapshot, nil
	}
	store, err := state.Load(filepath.Join(configuration.StateDir, "state.json"))
	if err != nil {
		return Snapshot{}, err
	}
	active, err := lock.Active("GitConverge")
	if err != nil {
		return Snapshot{}, err
	}
	runtime.SyncActive = active
	return Build(configuration, store, runtime, time.Now()), nil
}
