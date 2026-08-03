package runlog

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendRemovesExpiredEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	policy := Policy{MaxAge: 24 * time.Hour, MaxBytes: 1024 * 1024}
	if err := Append(path, testEntry(now.Add(-48*time.Hour), "old", ""), policy); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, testEntry(now, "current", ""), policy); err != nil {
		t.Fatal(err)
	}
	entries, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Repositories[0].ID != "current" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestAppendKeepsNewestEntriesWithinSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	policy := Policy{MaxAge: 24 * time.Hour, MaxBytes: 700}
	for index, id := range []string{"one", "two", "three"} {
		if err := Append(path, testEntry(now.Add(time.Duration(index)*time.Minute), id, strings.Repeat(id, 80)), policy); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 || entries[len(entries)-1].Repositories[0].ID != "three" {
		t.Fatalf("entries = %+v", entries)
	}
	if len(entries) >= 3 {
		t.Fatalf("size policy did not remove old entries: %d", len(entries))
	}
}

func TestLimitMessageRedactsCredentialsAndCapsLength(t *testing.T) {
	message := "fetch https://user:token@example.invalid/repo?access_token=query-secret\nwebhook = \"https://hooks.invalid/private\"\nAuthorization = Bearer bearer-secret\n" + strings.Repeat("x", 5000)
	limited := LimitMessage(message)
	for _, secret := range []string{"user:token", "query-secret", "hooks.invalid/private", "bearer-secret"} {
		if strings.Contains(limited, secret) {
			t.Fatalf("secret %q was retained: %s", secret, limited)
		}
	}
	if len([]rune(limited)) != 4097 {
		t.Fatalf("length = %d", len([]rune(limited)))
	}
}

func testEntry(at time.Time, id, message string) Entry {
	return Entry{
		StartedAt: at, CompletedAt: at.Add(time.Second), DurationMS: 1000, Status: "synced",
		Repositories: []Repository{{ID: id, Status: "synced", Action: "fetch", Message: message}},
	}
}
