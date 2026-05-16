package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/mappingcli"
	"github.com/mptooling/notifycat/internal/store"
)

// fakeMappingsValidator is the mock dispatch tests hand to runValidate. It
// satisfies mappingcli.MappingsValidator and records the inputs it sees so
// tests can assert dispatch routed correctly.
type fakeMappingsValidator struct {
	called    bool
	gotTarget string
	code      int
}

func (f *fakeMappingsValidator) Validate(_ context.Context, target string, _, _ io.Writer) int {
	f.called = true
	f.gotTarget = target
	return f.code
}

// panickingValidator fails the test if dispatch ever routes a non-validate
// subcommand through it. Useful for asserting routing isolation.
type panickingValidator struct{ t *testing.T }

func (p panickingValidator) Validate(_ context.Context, _ string, _, _ io.Writer) int {
	p.t.Helper()
	p.t.Fatal("validator.Validate must not be called for non-validate subcommands")
	return 0
}

func testRepo(t *testing.T) *store.RepoMappings {
	t.Helper()
	return store.NewRepoMappings(store.NewTestDB(t))
}

func TestDispatch_Add_InvalidRepositoryRejected(t *testing.T) {
	repo := testRepo(t)
	var out, errOut bytes.Buffer

	code := dispatch([]string{"add", "invalid-repo-format", "C123", "@a"}, repo, panickingValidator{t}, &out, &errOut)
	if code == 0 {
		t.Fatalf("add with invalid repo accepted; stderr=%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "repository") {
		t.Errorf("stderr should mention 'repository': %q", errOut.String())
	}
}

func TestDispatch_Add_InvalidChannelRejected(t *testing.T) {
	repo := testRepo(t)
	var out, errOut bytes.Buffer

	code := dispatch([]string{"add", "octo/widget", "not-a-channel", "@a"}, repo, panickingValidator{t}, &out, &errOut)
	if code == 0 {
		t.Fatalf("add with invalid channel accepted; stderr=%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "channel") {
		t.Errorf("stderr should mention 'channel': %q", errOut.String())
	}
}

func TestDispatch_Add_EmptyMentionsAllowed(t *testing.T) {
	repo := testRepo(t)
	var out, errOut bytes.Buffer

	code := dispatch([]string{"add", "octo/widget", "C123ABCDE", ""}, repo, panickingValidator{t}, &out, &errOut)
	if code != 0 {
		t.Fatalf("add with empty mentions exit = %d; stderr=%s", code, errOut.String())
	}
}

func TestDispatch_NoArgs(t *testing.T) {
	repo := testRepo(t)
	var out, errOut bytes.Buffer

	code := dispatch(nil, repo, panickingValidator{t}, &out, &errOut)
	if code == 0 {
		t.Fatal("no-args dispatch returned 0")
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Errorf("stderr should print usage: %q", errOut.String())
	}
}

func TestDispatch_UnknownSubcommand(t *testing.T) {
	repo := testRepo(t)
	var out, errOut bytes.Buffer

	code := dispatch([]string{"unknown"}, repo, panickingValidator{t}, &out, &errOut)
	if code == 0 {
		t.Fatal("unknown subcommand returned 0")
	}
	if !strings.Contains(errOut.String(), "unknown") {
		t.Errorf("stderr should mention 'unknown': %q", errOut.String())
	}
}

func TestDispatch_Validate_RoutesTargetToValidator(t *testing.T) {
	repo := testRepo(t)
	fv := &fakeMappingsValidator{code: 0}
	var out, errOut bytes.Buffer

	code := dispatch([]string{"validate", "acme/widgets"}, repo, fv, &out, &errOut)
	if code != 0 {
		t.Fatalf("validate exit = %d; stderr=%s", code, errOut.String())
	}
	if !fv.called {
		t.Fatal("validator.Validate was not called")
	}
	if fv.gotTarget != "acme/widgets" {
		t.Errorf("validator got target %q; want %q", fv.gotTarget, "acme/widgets")
	}
}

func TestDispatch_Validate_NoTargetForwardsEmpty(t *testing.T) {
	repo := testRepo(t)
	fv := &fakeMappingsValidator{code: 0}
	var out, errOut bytes.Buffer

	code := dispatch([]string{"validate"}, repo, fv, &out, &errOut)
	if code != 0 {
		t.Fatalf("validate (no target) exit = %d; stderr=%s", code, errOut.String())
	}
	if !fv.called || fv.gotTarget != "" {
		t.Errorf("expected validator called with empty target, got called=%v target=%q", fv.called, fv.gotTarget)
	}
}

func TestDispatch_Validate_PropagatesExitCode(t *testing.T) {
	repo := testRepo(t)
	fv := &fakeMappingsValidator{code: 1}
	var out, errOut bytes.Buffer

	code := dispatch([]string{"validate", "a/b"}, repo, fv, &out, &errOut)
	if code != 1 {
		t.Fatalf("validate exit = %d; want 1", code)
	}
}

func TestDispatch_Validate_TooManyArgs(t *testing.T) {
	repo := testRepo(t)
	var out, errOut bytes.Buffer

	code := dispatch([]string{"validate", "a/b", "c/d"}, repo, panickingValidator{t}, &out, &errOut)
	if code != 2 {
		t.Fatalf("validate too-many-args exit = %d; want 2", code)
	}
}

func TestSplitMentions(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"   ", []string{}},
		{"@a", []string{"@a"}},
		{"@a, @b , @c", []string{"@a", "@b", "@c"}},
		{",,@a,,", []string{"@a"}},
	}
	for _, c := range cases {
		got := splitMentions(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitMentions(%q) = %v; want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitMentions(%q)[%d] = %q; want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// Compile-time guarantees that the test doubles stay in sync with the
// MappingsValidator interface they pretend to implement.
var (
	_ mappingcli.MappingsValidator = (*fakeMappingsValidator)(nil)
	_ mappingcli.MappingsValidator = panickingValidator{}
)
