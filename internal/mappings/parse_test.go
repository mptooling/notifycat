package mappings

import (
	"strings"
	"testing"
)

const validYAML = `
mappings:
  acme:
    channel: C0123ABCDE
    mentions: ["@alice", "@bob"]
    repositories:
      - api
      - web
  beta:
    channel: C0456FGHIJ
    mentions: []
    repositories: "*"
`

func TestParse_Valid(t *testing.T) {
	f, err := Parse(strings.NewReader(validYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Mappings) != 2 {
		t.Fatalf("orgs = %d; want 2", len(f.Mappings))
	}
	acme := f.Mappings["acme"]
	if acme.Channel != "C0123ABCDE" || len(acme.Mentions) != 2 || len(acme.Repositories.List) != 2 {
		t.Errorf("acme parsed wrong: %+v", acme)
	}
	if !f.Mappings["beta"].Repositories.All {
		t.Errorf("beta should be wildcard")
	}
}

func TestParse_UnknownTopLevelKey(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings: {}
something_else: true
`))
	if err == nil || !strings.Contains(err.Error(), "field") {
		t.Fatalf("expected unknown-field error; got %v", err)
	}
}

func TestParse_UnknownOrgKey(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  acme:
    channel: C0123ABCDE
    mentions: []
    repositories: ["x"]
    typo_field: 1
`))
	if err == nil || !strings.Contains(err.Error(), "field") {
		t.Fatalf("expected unknown-field error; got %v", err)
	}
}

func TestParse_BadOrgKey(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  "bad org name":
    channel: C0123ABCDE
    mentions: []
    repositories: ["x"]
`))
	if err == nil || !strings.Contains(err.Error(), "org") {
		t.Fatalf("expected org-name error; got %v", err)
	}
}

func TestParse_BadChannel(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  acme:
    channel: not-a-channel
    mentions: []
    repositories: ["x"]
`))
	if err == nil || !strings.Contains(err.Error(), "channel") {
		t.Fatalf("expected channel error; got %v", err)
	}
}

func TestParse_NilMentionsRejected(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  acme:
    channel: C0123ABCDE
    mentions:
    repositories: ["x"]
`))
	if err == nil || !strings.Contains(err.Error(), "mentions") {
		t.Fatalf("expected mentions error; got %v", err)
	}
}

func TestParse_DuplicateRepoInList(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  acme:
    channel: C0123ABCDE
    mentions: []
    repositories: ["api", "api"]
`))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error; got %v", err)
	}
}

func TestParse_BadRepoName(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  acme:
    channel: C0123ABCDE
    mentions: []
    repositories: ["bad/name"]
`))
	if err == nil || !strings.Contains(err.Error(), "repository") {
		t.Fatalf("expected repo-name error; got %v", err)
	}
}
