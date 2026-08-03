package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

func Save(path string, configuration Config) (Config, error) {
	defaults, err := Default(path)
	if err != nil {
		return Config{}, err
	}
	applyDefaults(&configuration, defaults)
	configuration.SourcePath = ""
	normalized, err := Normalize(configuration, defaults.SourcePath)
	if err != nil {
		return Config{}, err
	}
	content, err := toml.Marshal(configuration)
	if err != nil {
		return Config{}, err
	}
	if err := os.MkdirAll(filepath.Dir(defaults.SourcePath), 0o700); err != nil {
		return Config{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(defaults.SourcePath), "config-*.tmp")
	if err != nil {
		return Config{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return Config{}, err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return Config{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return Config{}, err
	}
	if err := temporary.Close(); err != nil {
		return Config{}, err
	}
	if err := replaceFile(temporaryPath, defaults.SourcePath); err != nil {
		return Config{}, fmt.Errorf("replace configuration: %w", err)
	}
	return normalized, nil
}
