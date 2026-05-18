package mappingcli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/validate"
)

type stubChecker struct {
	fn    func(ctx context.Context, repository string) validate.Report
	calls []string
}

func (s *stubChecker) Validate(ctx context.Context, repository string) validate.Report {
	s.calls = append(s.calls, repository)
	if s.fn != nil {
		return s.fn(ctx, repository)
	}
	return passingReport(repository)
}

func passingReport(repository string) validate.Report {
	return validate.Report{
		Repository: repository,
		Checks:     []validate.CheckResult{{Name: "x", Status: validate.StatusOK, Detail: "ok"}},
	}
}

func failingReport(repository string) validate.Report {
	return validate.Report{
		Repository: repository,
		Checks:     []validate.CheckResult{{Name: "x", Status: validate.StatusFail, Detail: "boom"}},
	}
}

func writeMappingsFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mappings.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	return path
}

func loadProvider(t *testing.T, body string) (*mappings.Provider, string) {
	t.Helper()
	path := writeMappingsFile(t, body)
	p, err := mappings.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return p, mappings.LockPath(path)
}

func fixedClock() func() time.Time {
	now := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	return func() time.Time { return now }
}

const explicitYAML = `mappings:
  acme:
    channel: C0123ABCDE
    mentions: ["@a"]
    repositories:
      - api
      - web
`

const wildcardYAML = `mappings:
  beta:
    channel: C0456FGHIJ
    mentions: ["@b"]
    repositories: "*"
`

func TestMappingsValidator_Targeted_AllPass_WritesLock(t *testing.T) {
	p, lockPath := loadProvider(t, explicitYAML)
	sc := &stubChecker{}
	v := NewMappingsValidator(p, sc, nil, lockPath, fixedClock())
	var out, errOut bytes.Buffer

	if code := v.Validate(context.Background(), "acme/api", false, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d; stderr=%s", code, errOut.String())
	}
	if len(sc.calls) != 1 || sc.calls[0] != "acme/api" {
		t.Errorf("checker calls = %v", sc.calls)
	}
	lock, err := mappings.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if _, ok := lock.Entries["acme/api"]; !ok {
		t.Errorf("lock missing acme/api: %+v", lock.Entries)
	}
}

func TestMappingsValidator_Targeted_Failure_DoesNotWriteLock(t *testing.T) {
	p, lockPath := loadProvider(t, explicitYAML)
	sc := &stubChecker{fn: func(_ context.Context, r string) validate.Report { return failingReport(r) }}
	v := NewMappingsValidator(p, sc, nil, lockPath, fixedClock())
	var out, errOut bytes.Buffer

	if code := v.Validate(context.Background(), "acme/api", false, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d; want 1", code)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock should not have been written; err=%v", err)
	}
}

func TestMappingsValidator_Targeted_WildcardOrg_SkipsLockUpdate(t *testing.T) {
	p, lockPath := loadProvider(t, wildcardYAML)
	sc := &stubChecker{}
	v := NewMappingsValidator(p, sc, nil, lockPath, fixedClock())
	var out, errOut bytes.Buffer

	if code := v.Validate(context.Background(), "beta/anything", false, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d; stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("wildcard-resolved targeted run must not write the lock; err=%v", err)
	}
}

func TestMappingsValidator_Full_EmptyMappings(t *testing.T) {
	p, lockPath := loadProvider(t, "mappings: {}\n")
	sc := &stubChecker{}
	v := NewMappingsValidator(p, sc, nil, lockPath, fixedClock())
	var out, errOut bytes.Buffer

	if code := v.Validate(context.Background(), "", false, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "no mappings to validate") {
		t.Errorf("expected friendly empty-state message; got %q", out.String())
	}
	if len(sc.calls) != 0 {
		t.Errorf("checker should not have been called: %v", sc.calls)
	}
}

func TestMappingsValidator_Full_NoLock_ValidatesAll_WritesLock(t *testing.T) {
	p, lockPath := loadProvider(t, explicitYAML)
	sc := &stubChecker{}
	v := NewMappingsValidator(p, sc, nil, lockPath, fixedClock())
	var out, errOut bytes.Buffer

	if code := v.Validate(context.Background(), "", false, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d; stderr=%s", code, errOut.String())
	}
	if len(sc.calls) != 2 {
		t.Errorf("expected 2 calls (api, web); got %v", sc.calls)
	}
	lock, err := mappings.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if _, ok := lock.Entries["acme/api"]; !ok {
		t.Errorf("lock missing acme/api: %+v", lock.Entries)
	}
	if _, ok := lock.Entries["acme/web"]; !ok {
		t.Errorf("lock missing acme/web: %+v", lock.Entries)
	}
}

func TestMappingsValidator_Full_UpToDateLock_SkipsValidation(t *testing.T) {
	p, lockPath := loadProvider(t, explicitYAML)
	sc := &stubChecker{}
	// Prime the lock with the current entry hashes.
	clock := fixedClock()
	entries := p.Entries()
	prior := mappings.Lock{Version: mappings.LockVersion, Entries: map[string]mappings.LockEntry{}}
	for _, e := range entries {
		prior.Entries[e.Key()] = mappings.LockEntry{SHA256: e.Hash(), ValidatedAt: clock()}
	}
	if err := mappings.WriteLock(lockPath, prior); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	v := NewMappingsValidator(p, sc, nil, lockPath, clock)
	var out, errOut bytes.Buffer

	if code := v.Validate(context.Background(), "", false, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(sc.calls) != 0 {
		t.Errorf("expected zero calls; got %v", sc.calls)
	}
	if !strings.Contains(out.String(), "lock is up to date") {
		t.Errorf("expected up-to-date hint; got %q", out.String())
	}
}

func TestMappingsValidator_Full_Force_IgnoresLock(t *testing.T) {
	p, lockPath := loadProvider(t, explicitYAML)
	clock := fixedClock()
	// Prime lock with up-to-date entries.
	entries := p.Entries()
	prior := mappings.Lock{Version: mappings.LockVersion, Entries: map[string]mappings.LockEntry{}}
	for _, e := range entries {
		prior.Entries[e.Key()] = mappings.LockEntry{SHA256: e.Hash(), ValidatedAt: clock()}
	}
	if err := mappings.WriteLock(lockPath, prior); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	sc := &stubChecker{}
	v := NewMappingsValidator(p, sc, nil, lockPath, clock)
	var out, errOut bytes.Buffer

	if code := v.Validate(context.Background(), "", true, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d; stderr=%s", code, errOut.String())
	}
	if len(sc.calls) != 2 {
		t.Errorf("force should revalidate everything; got calls=%v", sc.calls)
	}
}

func TestMappingsValidator_Full_PartialFailure_OnlySuccessesEnterLock(t *testing.T) {
	p, lockPath := loadProvider(t, explicitYAML)
	sc := &stubChecker{fn: func(_ context.Context, r string) validate.Report {
		if r == "acme/api" {
			return failingReport(r)
		}
		return passingReport(r)
	}}
	v := NewMappingsValidator(p, sc, nil, lockPath, fixedClock())
	var out, errOut bytes.Buffer

	if code := v.Validate(context.Background(), "", false, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d; want 1 (one entry failed)", code)
	}
	lock, err := mappings.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if _, ok := lock.Entries["acme/api"]; ok {
		t.Errorf("acme/api failed; should not be in lock: %+v", lock.Entries)
	}
	if _, ok := lock.Entries["acme/web"]; !ok {
		t.Errorf("acme/web passed; should be in lock: %+v", lock.Entries)
	}
}
