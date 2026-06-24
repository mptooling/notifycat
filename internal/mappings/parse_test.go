package mappings

import (
	"strings"
	"testing"
)

func TestParse_PerRepoTiers_OK(t *testing.T) {
	f, err := Parse(strings.NewReader(`
mappings:
  acme:
    api:
      channel: C0API
    web:
      channel: C0WEB
    "*":
      channel: C0DEFAULT
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Mappings["acme"]["api"].Channel != "C0API" {
		t.Errorf("api channel = %q", f.Mappings["acme"]["api"].Channel)
	}
}

func TestParse_InheritsChannelFromStar(t *testing.T) {
	// api sets no channel but org/* does — valid (api inherits at resolve).
	if _, err := Parse(strings.NewReader(`
mappings:
  acme:
    api:
      mentions: ["<@U1>"]
    "*":
      channel: C0DEFAULT
`)); err != nil {
		t.Fatalf("Parse should accept channel inherited from *: %v", err)
	}
}

func TestParse_RepoWithoutChannelAndNoStarRejected(t *testing.T) {
	if _, err := Parse(strings.NewReader(`
mappings:
  acme:
    api:
      mentions: ["<@U1>"]
`)); err == nil {
		t.Fatal("expected error: api has no channel and no org/* to inherit from")
	}
}

func TestParse_BadChannelRejected(t *testing.T) {
	if _, err := Parse(strings.NewReader("mappings:\n  acme:\n    api:\n      channel: not-a-channel\n")); err == nil {
		t.Fatal("expected error for malformed channel")
	}
}

func TestParse_BadRepoKeyRejected(t *testing.T) {
	if _, err := Parse(strings.NewReader("mappings:\n  acme:\n    \"a/b\":\n      channel: C0API\n")); err == nil {
		t.Fatal("expected error for repo key containing /")
	}
}

func TestParse_EmptyOrgRejected(t *testing.T) {
	if _, err := Parse(strings.NewReader("mappings:\n  acme: {}\n")); err == nil {
		t.Fatal("expected error for org with no tiers")
	}
}
