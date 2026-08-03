package dashboard

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/runlog"
)

type LogDocument struct {
	Path    string         `json:"path"`
	Entries []runlog.Entry `json:"entries"`
}

func LoadLogs(configPath string, limit int) (LogDocument, error) {
	configuration, err := config.Load(configPath)
	if errors.Is(err, os.ErrNotExist) {
		configuration, err = config.Default(configPath)
	}
	if err != nil {
		return LogDocument{}, err
	}
	path := filepath.Join(configuration.StateDir, runlog.FileName)
	entries, err := runlog.Read(path)
	if err != nil {
		return LogDocument{}, err
	}
	if entries == nil {
		entries = []runlog.Entry{}
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	return LogDocument{Path: path, Entries: entries}, nil
}
