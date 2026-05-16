package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/mappingcli"
	"github.com/mptooling/notifycat/internal/store"
	"github.com/mptooling/notifycat/internal/validate"
)

func noopFactory() mappingcli.ValidatorFactory {
	return func(_ *store.RepoMappings) (mappingcli.Validator, error) {
		return stubValidator{}, nil
	}
}

type stubValidator struct{}

func (stubValidator) Validate(_ context.Context, repository string) validate.Report {
	return validate.Report{Repository: repository}
}
func (stubValidator) ValidateAll(_ context.Context) ([]validate.Report, error) { return nil, nil }

func TestDispatch_Add_InvalidRepositoryRejected(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := dispatch([]string{"add", "invalid-repo-format", "C123", "@a"}, db, &out, &errOut, noopFactory())
	if code == 0 {
		t.Fatalf("add with invalid repo accepted; stderr=%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "repository") {
		t.Errorf("stderr should mention 'repository': %q", errOut.String())
	}
}

func TestDispatch_Add_InvalidChannelRejected(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := dispatch([]string{"add", "octo/widget", "not-a-channel", "@a"}, db, &out, &errOut, noopFactory())
	if code == 0 {
		t.Fatalf("add with invalid channel accepted; stderr=%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "channel") {
		t.Errorf("stderr should mention 'channel': %q", errOut.String())
	}
}

func TestDispatch_Add_EmptyMentionsAllowed(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := dispatch([]string{"add", "octo/widget", "C123ABCDE", ""}, db, &out, &errOut, noopFactory())
	if code != 0 {
		t.Fatalf("add with empty mentions exit = %d; stderr=%s", code, errOut.String())
	}
}

func TestDispatch_NoArgs(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := dispatch(nil, db, &out, &errOut, noopFactory())
	if code == 0 {
		t.Fatal("no-args dispatch returned 0")
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Errorf("stderr should print usage: %q", errOut.String())
	}
}

func TestDispatch_UnknownSubcommand(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := dispatch([]string{"unknown"}, db, &out, &errOut, noopFactory())
	if code == 0 {
		t.Fatal("unknown subcommand returned 0")
	}
	if !strings.Contains(errOut.String(), "unknown") {
		t.Errorf("stderr should mention 'unknown': %q", errOut.String())
	}
}

func TestDispatch_Validate_TooManyArgs(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := dispatch([]string{"validate", "a/b", "c/d"}, db, &out, &errOut, noopFactory())
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
