package runlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const FileName = "runs.jsonl"

var (
	credentialURL    = regexp.MustCompile(`(?i)\b(https?://)([^/\s@]+)@`)
	secretAssignment = regexp.MustCompile(`(?im)\b(webhook|secret|token|password|authorization)(\s*=\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,\r\n}]+)`)
	secretQuery      = regexp.MustCompile(`(?i)([?&](?:access_token|token|secret|key)=)[^&\s"']+`)
	bearerValue      = regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._~+/=-]+`)
)

type Policy struct {
	MaxAge   time.Duration
	MaxBytes int64
}

type Entry struct {
	Version      int          `json:"version"`
	StartedAt    time.Time    `json:"startedAt"`
	CompletedAt  time.Time    `json:"completedAt"`
	DurationMS   int64        `json:"durationMs"`
	Status       string       `json:"status"`
	Error        string       `json:"error,omitempty"`
	Repositories []Repository `json:"repositories"`
}

type Repository struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Action  string `json:"action"`
	Head    string `json:"head,omitempty"`
	Dirty   bool   `json:"dirty"`
	Message string `json:"message,omitempty"`
}

func LimitMessage(message string) string {
	message = credentialURL.ReplaceAllString(message, `${1}<redacted>@`)
	message = bearerValue.ReplaceAllString(message, `${1}<redacted>`)
	message = secretAssignment.ReplaceAllString(message, `${1}${2}<redacted>`)
	message = secretQuery.ReplaceAllString(message, `${1}<redacted>`)
	characters := []rune(message)
	if len(characters) <= 4096 {
		return message
	}
	return string(characters[:4096]) + "…"
}

func Append(path string, entry Entry, policy Policy) error {
	if entry.Version == 0 {
		entry.Version = 1
	}
	if entry.CompletedAt.IsZero() {
		entry.CompletedAt = time.Now()
	}
	content, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return compact(path, entry.CompletedAt, policy)
}

func Read(path string) ([]Entry, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries := []Entry{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

func compact(path string, now time.Time, policy Policy) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	type line struct {
		content []byte
	}
	cutoff := time.Time{}
	if policy.MaxAge > 0 {
		cutoff = now.Add(-policy.MaxAge)
	}
	lines := []line{}
	totalBytes := int64(0)
	changed := false
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		content := append([]byte(nil), scanner.Bytes()...)
		var header struct {
			CompletedAt time.Time `json:"completedAt"`
		}
		if err := json.Unmarshal(content, &header); err != nil || header.CompletedAt.IsZero() {
			changed = true
			continue
		}
		if !cutoff.IsZero() && header.CompletedAt.Before(cutoff) {
			changed = true
			continue
		}
		content = append(content, '\n')
		lines = append(lines, line{content: content})
		totalBytes += int64(len(content))
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for policy.MaxBytes > 0 && totalBytes > policy.MaxBytes && len(lines) > 1 {
		totalBytes -= int64(len(lines[0].content))
		lines = lines[1:]
		changed = true
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !changed && info.Size() == totalBytes {
		return nil
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), "runs-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	writer := bufio.NewWriter(temporary)
	for _, item := range lines {
		if _, err := writer.Write(item.content); err != nil {
			temporary.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace compacted run log: %w", err)
	}
	return nil
}
