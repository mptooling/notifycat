package mappings_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/mptooling/notifycat/internal/mappings"
)

func decodeOrg(t *testing.T, body string) mappings.Org {
	t.Helper()
	var o mappings.Org
	dec := yaml.NewDecoder(strings.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return o
}

func TestRepoConfig_ChannelAndMentionsPresent(t *testing.T) {
	o := decodeOrg(t, `
api:
  channel: C0API
  mentions: ["<@U1>"]
"*":
  channel: C0STAR
`)
	api, ok := o["api"]
	if !ok {
		t.Fatal("missing api tier")
	}
	if api.Channel != "C0API" {
		t.Errorf("api.Channel = %q; want C0API", api.Channel)
	}
	if !api.MentionsPresent || len(api.Mentions) != 1 || api.Mentions[0] != "<@U1>" {
		t.Errorf("api mentions = %+v present=%v", api.Mentions, api.MentionsPresent)
	}
	star := o["*"]
	if star.Channel != "C0STAR" || star.MentionsPresent {
		t.Errorf("star = %+v; want channel C0STAR, mentions absent", star)
	}
}

func TestRepoConfig_EmptyMentionsIsPresent(t *testing.T) {
	o := decodeOrg(t, "api:\n  channel: C0API\n  mentions: []\n")
	if !o["api"].MentionsPresent || len(o["api"].Mentions) != 0 {
		t.Errorf("mentions: [] should be present+empty; got %+v", o["api"])
	}
}

func TestRepoConfig_NullMentionsRejected(t *testing.T) {
	var o mappings.Org
	dec := yaml.NewDecoder(strings.NewReader("api:\n  channel: C0API\n  mentions: null\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err == nil {
		t.Fatal("expected error for mentions: null")
	}
}

func TestRepoConfig_UnknownKeyRejected(t *testing.T) {
	var o mappings.Org
	dec := yaml.NewDecoder(strings.NewReader("api:\n  channel: C0API\n  bogus: x\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err == nil {
		t.Fatal("expected error for unknown tier key")
	}
}
