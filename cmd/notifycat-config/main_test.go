package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/config"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
)

func testProvider(t *testing.T) *routingapp.Provider {
	t.Helper()

	mappings := map[string]routingdomain.Org{
		"acme": {
			"api": {Channel: "C0123ABCDE", Mentions: []string{"@a"}, MentionsPresent: true},
			"web": {Channel: "C0123ABCDE", Mentions: []string{"@a"}, MentionsPresent: true},
		},
	}
	return routingapp.NewProvider(routingdomain.Defaults{}, mappings, nil)
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

// refusingValidator fails the test if dispatch ever routes a non-validate
// subcommand through it.
type refusingValidator struct{ t *testing.T }

func (r refusingValidator) Validate(_ context.Context, _ string, _ bool, _, _ io.Writer) int {
	r.t.Helper()
	assert.Fail(r.t, "Validate must not be called for a non-validate subcommand")
	return 0
}

var (
	_ mappingsValidator = (*fakeMappingsValidator)(nil)
	_ mappingsValidator = refusingValidator{}
)

// runDispatch drives the CLI with the given args and returns its exit code,
// stdout, and stderr.
func runDispatch(t *testing.T, args []string, validator mappingsValidator) (int, string, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := dispatch(args, testProvider(t), validator, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestDispatch_NoArgs(t *testing.T) {
	code, _, stderr := runDispatch(t, nil, refusingValidator{t})

	assert.NotZero(t, code)
	assert.Contains(t, stderr, "usage")
}

func TestDispatch_UnknownSubcommand(t *testing.T) {
	code, _, stderr := runDispatch(t, []string{"unknown"}, refusingValidator{t})

	assert.NotZero(t, code)
	assert.Contains(t, stderr, "unknown")
}

func TestDispatch_List_RendersProvider(t *testing.T) {
	code, stdout, _ := runDispatch(t, []string{"list"}, refusingValidator{t})

	require.Zero(t, code)
	for _, want := range []string{"acme", "api", "web", "C0123ABCDE", "@a"} {
		assert.Contains(t, stdout, want)
	}
}

func TestDispatch_Validate_RoutesTarget(t *testing.T) {
	validator := &fakeMappingsValidator{}

	code, _, _ := runDispatch(t, []string{"validate", "acme/api"}, validator)

	require.Zero(t, code)
	assert.True(t, validator.called)
	assert.Equal(t, "acme/api", validator.gotTarget)
	assert.False(t, validator.gotForce)
}

func TestDispatch_Validate_NoTargetForwardsEmpty(t *testing.T) {
	validator := &fakeMappingsValidator{}

	code, _, _ := runDispatch(t, []string{"validate"}, validator)

	require.Zero(t, code)
	assert.True(t, validator.called)
	assert.Empty(t, validator.gotTarget)
	assert.False(t, validator.gotForce)
}

func TestDispatch_Validate_ForceFlag(t *testing.T) {
	validator := &fakeMappingsValidator{}

	code, _, _ := runDispatch(t, []string{"validate", "--force"}, validator)

	require.Zero(t, code)
	assert.True(t, validator.called)
	assert.Empty(t, validator.gotTarget)
	assert.True(t, validator.gotForce)
}

func TestDispatch_Validate_ForceWithTarget(t *testing.T) {
	validator := &fakeMappingsValidator{}

	code, _, _ := runDispatch(t, []string{"validate", "--force", "acme/api"}, validator)

	require.Zero(t, code)
	assert.Equal(t, "acme/api", validator.gotTarget)
	assert.True(t, validator.gotForce)
}

func TestDispatch_Validate_PropagatesExitCode(t *testing.T) {
	validator := &fakeMappingsValidator{code: 1}

	code, _, _ := runDispatch(t, []string{"validate", "a/b"}, validator)

	assert.Equal(t, 1, code)
}

func TestDispatch_Validate_TooManyPositional(t *testing.T) {
	code, _, _ := runDispatch(t, []string{"validate", "a/b", "c/d"}, refusingValidator{t})

	assert.Equal(t, 2, code, "a usage error exits 2")
}

func TestDispatch_Validate_UnknownFlag(t *testing.T) {
	code, _, _ := runDispatch(t, []string{"validate", "--bogus"}, refusingValidator{t})

	assert.Equal(t, 2, code)
}

func TestPathTokenWarning(t *testing.T) {
	file, err := routinginfra.Parse(strings.NewReader(
		"mappings:\n  acme:\n    mono:\n      channel: C0BASE00000\n      paths:\n        \"/src\": {mentions: []}\n"))
	require.NoError(t, err)
	withPaths := routingapp.NewProvider(routingdomain.Defaults{}, file.Mappings, nil)

	assert.Contains(t, pathTokenWarning(withPaths, config.Config{}), "GITHUB_TOKEN",
		"path routing without a token is inert, so the CLI says so")
	assert.Empty(t, pathTokenWarning(withPaths, config.Config{GitHubToken: config.Secret("t")}))
	assert.Empty(t, pathTokenWarning(testProvider(t), config.Config{}), "no path rules, nothing to warn about")
	assert.Contains(t, pathTokenWarning(withPaths, config.Config{GitProvider: "bitbucket"}), "BITBUCKET_TOKEN",
		"a bitbucket deployment names its own token")
}
