package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
)

func testProvider(t *testing.T) *routingapp.Provider {
	t.Helper()
	m := map[string]routingdomain.Org{
		"acme": {
			"api": {Channel: "C0123ABCDE", Mentions: []string{"@a"}, MentionsPresent: true},
			"web": {Channel: "C0123ABCDE", Mentions: []string{"@a"}, MentionsPresent: true},
		},
	}
	return routingapp.NewProvider(routingdomain.Defaults{}, m, nil)
}

// fakeMappingsValidator records inputs so dispatch tests can assert the
// routing layer forwarded target + force unchanged.
type fakeMappingsValidator struct {
	called    bool
	gotTarget string
	gotForce  bool
	code      int
}

func (f *fakeMappingsValidator) Validate(_ context.Context, target string, force bool, _, _ io.Writer) int {
	f.called = true
	f.gotTarget = target
	f.gotForce = force
	return f.code
}

// panickingValidator fails the test if dispatch ever routes a non-validate
// subcommand through it.
type panickingValidator struct{ t *testing.T }

func (p panickingValidator) Validate(_ context.Context, _ string, _ bool, _, _ io.Writer) int {
	p.t.Helper()
	p.t.Fatal("validator.Validate must not be called for non-validate subcommands")
	return 0
}

var (
	_ mappingsValidator = (*fakeMappingsValidator)(nil)
	_ mappingsValidator = panickingValidator{}
)

func TestDispatch_NoArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatch(nil, testProvider(t), panickingValidator{t}, &out, &errOut)
	if code == 0 {
		t.Fatal("no-args dispatch returned 0")
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Errorf("stderr should print usage: %q", errOut.String())
	}
}

func TestDispatch_UnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatch([]string{"unknown"}, testProvider(t), panickingValidator{t}, &out, &errOut)
	if code == 0 {
		t.Fatal("unknown subcommand returned 0")
	}
	if !strings.Contains(errOut.String(), "unknown") {
		t.Errorf("stderr should mention 'unknown': %q", errOut.String())
	}
}

func TestDispatch_List_RendersProvider(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatch([]string{"list"}, testProvider(t), panickingValidator{t}, &out, &errOut)
	if code != 0 {
		t.Fatalf("list exit = %d; stderr=%s", code, errOut.String())
	}
	for _, want := range []string{"acme", "api", "web", "C0123ABCDE", "@a"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list output missing %q: %q", want, out.String())
		}
	}
}

func TestDispatch_Validate_RoutesTarget(t *testing.T) {
	fv := &fakeMappingsValidator{code: 0}
	var out, errOut bytes.Buffer
	code := dispatch([]string{"validate", "acme/api"}, testProvider(t), fv, &out, &errOut)
	if code != 0 {
		t.Fatalf("validate exit = %d; stderr=%s", code, errOut.String())
	}
	if !fv.called || fv.gotTarget != "acme/api" || fv.gotForce {
		t.Errorf("validator got called=%v target=%q force=%v", fv.called, fv.gotTarget, fv.gotForce)
	}
}

func TestDispatch_Validate_NoTargetForwardsEmpty(t *testing.T) {
	fv := &fakeMappingsValidator{code: 0}
	var out, errOut bytes.Buffer
	code := dispatch([]string{"validate"}, testProvider(t), fv, &out, &errOut)
	if code != 0 {
		t.Fatalf("validate exit = %d; stderr=%s", code, errOut.String())
	}
	if !fv.called || fv.gotTarget != "" || fv.gotForce {
		t.Errorf("expected validator called with empty target, no force; got %+v", fv)
	}
}

func TestDispatch_Validate_ForceFlag(t *testing.T) {
	fv := &fakeMappingsValidator{code: 0}
	var out, errOut bytes.Buffer
	code := dispatch([]string{"validate", "--force"}, testProvider(t), fv, &out, &errOut)
	if code != 0 {
		t.Fatalf("validate --force exit = %d; stderr=%s", code, errOut.String())
	}
	if !fv.called || fv.gotTarget != "" || !fv.gotForce {
		t.Errorf("expected force=true, empty target; got %+v", fv)
	}
}

func TestDispatch_Validate_ForceWithTarget(t *testing.T) {
	fv := &fakeMappingsValidator{code: 0}
	var out, errOut bytes.Buffer
	code := dispatch([]string{"validate", "--force", "acme/api"}, testProvider(t), fv, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d; stderr=%s", code, errOut.String())
	}
	if !fv.called || fv.gotTarget != "acme/api" || !fv.gotForce {
		t.Errorf("expected target+force; got %+v", fv)
	}
}

func TestDispatch_Validate_PropagatesExitCode(t *testing.T) {
	fv := &fakeMappingsValidator{code: 1}
	var out, errOut bytes.Buffer
	code := dispatch([]string{"validate", "a/b"}, testProvider(t), fv, &out, &errOut)
	if code != 1 {
		t.Fatalf("validate exit = %d; want 1", code)
	}
}

func TestDispatch_Validate_TooManyPositional(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatch([]string{"validate", "a/b", "c/d"}, testProvider(t), panickingValidator{t}, &out, &errOut)
	if code != 2 {
		t.Fatalf("too-many-args exit = %d; want 2", code)
	}
}

func TestDispatch_Validate_UnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatch([]string{"validate", "--bogus"}, testProvider(t), panickingValidator{t}, &out, &errOut)
	if code != 2 {
		t.Fatalf("unknown-flag exit = %d; want 2", code)
	}
}

func TestPathTokenWarning(t *testing.T) {
	f, err := routinginfra.Parse(strings.NewReader(
		"mappings:\n  acme:\n    mono:\n      channel: C0BASE00000\n      paths:\n        \"/src\": {mentions: []}\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	withPaths := routingapp.NewProvider(routingdomain.Defaults{}, f.Mappings, nil)

	if w := pathTokenWarning(withPaths, false); !strings.Contains(w, "GITHUB_TOKEN") {
		t.Errorf("paths + no token: got %q; want a GITHUB_TOKEN warning", w)
	}
	if w := pathTokenWarning(withPaths, true); w != "" {
		t.Errorf("paths + token: got %q; want no warning", w)
	}
	if w := pathTokenWarning(testProvider(t), false); w != "" {
		t.Errorf("no paths: got %q; want no warning", w)
	}
}
