package infrastructure

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

func TestLock_WriteThenRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mappings.lock")
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	want := Lock{
		Version: 1,
		Entries: map[string]LockEntry{
			"acme/api": {SHA256: "abc", ValidatedAt: now},
			"beta/*":   {SHA256: "def", ValidatedAt: now},
		},
	}
	if err := WriteLock(p, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadLock(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries["acme/api"].SHA256 != "abc" {
		t.Errorf("round trip wrong: %+v", got)
	}
}

func TestLock_Read_Missing(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadLock(filepath.Join(dir, "no.lock"))
	if err != nil {
		t.Fatalf("missing should not error; got %v", err)
	}
	if len(got.Entries) != 0 {
		t.Errorf("missing should produce empty lock; got %+v", got)
	}
}

func TestLock_Read_Malformed_ReturnsEmptyAndError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.lock")
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := ReadLock(p)
	if err == nil {
		t.Fatal("malformed should return an error so caller can warn")
	}
	if len(got.Entries) != 0 {
		t.Errorf("malformed should produce empty lock; got %+v", got)
	}
}

func TestDiffEntries(t *testing.T) {
	current := []domain.Entry{
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
		{Org: "acme", Repo: "web", Channel: "C1", Mentions: []string{}}, // new vs lock
		{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}},
	}
	lock := Lock{
		Version: 1,
		Entries: map[string]LockEntry{
			"acme/api": {SHA256: current[0].Hash()},
			"beta/*":   {SHA256: "stale-different-hash"}, // changed
			"old/dead": {SHA256: "x"},                    // stale
		},
	}
	d := DiffEntries(current, lock)
	needs := make(map[string]bool)
	for _, e := range d.Needs {
		needs[e.Key()] = true
	}
	if !needs["acme/web"] || !needs["beta/*"] {
		t.Errorf("Needs should include new (acme/web) and changed (beta/*); got %v", needs)
	}
	if needs["acme/api"] {
		t.Errorf("Needs should not include unchanged (acme/api)")
	}
	stale := make(map[string]bool)
	for _, k := range d.Stale {
		stale[k] = true
	}
	if !stale["old/dead"] || len(stale) != 1 {
		t.Errorf("Stale should be [old/dead]; got %v", stale)
	}
}

func TestMergeLock(t *testing.T) {
	old := Lock{
		Version: 1,
		Entries: map[string]LockEntry{
			"acme/api": {SHA256: "keep"},
			"old/dead": {SHA256: "x"},
		},
	}
	validated := map[string]LockEntry{
		"acme/web": {SHA256: "new"},
	}
	got := MergeLock(old, validated, []string{"old/dead"})
	if _, ok := got.Entries["old/dead"]; ok {
		t.Error("stale entry should be dropped")
	}
	if got.Entries["acme/api"].SHA256 != "keep" {
		t.Error("unchanged entry should remain")
	}
	if got.Entries["acme/web"].SHA256 != "new" {
		t.Error("validated entry should be added")
	}
}
