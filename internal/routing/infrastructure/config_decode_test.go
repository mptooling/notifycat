package infrastructure

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func decodeOrg(t *testing.T, body string) map[string]repoConfigWire {
	t.Helper()
	var o map[string]repoConfigWire
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
	var o map[string]repoConfigWire
	dec := yaml.NewDecoder(strings.NewReader("api:\n  channel: C0API\n  mentions: null\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err == nil {
		t.Fatal("expected error for mentions: null")
	}
}

func TestRepoConfig_UnknownKeyRejected(t *testing.T) {
	var o map[string]repoConfigWire
	dec := yaml.NewDecoder(strings.NewReader("api:\n  channel: C0API\n  bogus: x\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err == nil {
		t.Fatal("expected error for unknown tier key")
	}
}

func TestRepoConfig_BehavioralOverrides(t *testing.T) {
	o := decodeOrg(t, `
api:
  channel: C0API
  reactions:
    approved: shipit
    enabled: false
  reviews:
    ignore_ai_reviews: true
    dependabot_format: false
  digest:
    enabled: false
    schedule: "0 8 * * 1-5"
`)
	api := o["api"]
	if api.Reactions == nil || api.Reactions.Approved == nil || *api.Reactions.Approved != "shipit" {
		t.Fatalf("reactions.approved override missing: %+v", api.Reactions)
	}
	if api.Reactions.Enabled == nil || *api.Reactions.Enabled != false {
		t.Errorf("reactions.enabled override missing")
	}
	if api.IgnoreAIReviews == nil || *api.IgnoreAIReviews != true {
		t.Errorf("ignore_ai_reviews override missing")
	}
	if api.DependabotFormat == nil || *api.DependabotFormat != false {
		t.Errorf("dependabot_format override missing")
	}
	if api.Digest == nil || api.Digest.Enabled != false || api.Digest.Schedule != "0 8 * * 1-5" {
		t.Errorf("digest override missing: %+v", api.Digest)
	}
}

func TestRepoConfig_BehavioralAbsentMeansNil(t *testing.T) {
	api := decodeOrg(t, "api:\n  channel: C0API\n")["api"]
	if api.Reactions != nil || api.IgnoreAIReviews != nil || api.DependabotFormat != nil || api.Digest != nil {
		t.Errorf("absent behavioral keys should be nil (inherit): %+v", api)
	}
}

func TestRepoConfig_DigestTimezoneRejected(t *testing.T) {
	// timezone is a global-only knob (one cron location for the whole server);
	// setting it on a per-repo tier must fail rather than be silently ignored.
	var o map[string]repoConfigWire
	dec := yaml.NewDecoder(strings.NewReader("api:\n  channel: C0API\n  digest:\n    timezone: Europe/Kyiv\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err == nil {
		t.Fatal("expected error for per-repo digest.timezone")
	}
}

func TestRepoConfig_UnknownReactionKeyRejected(t *testing.T) {
	var o map[string]repoConfigWire
	dec := yaml.NewDecoder(strings.NewReader("api:\n  channel: C0API\n  reactions:\n    bogus: x\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err == nil {
		t.Fatal("expected error for unknown reactions key")
	}
}

func TestDecodeDigest_NullNodeIsAbsent(t *testing.T) {
	var doc struct {
		Digest yaml.Node `yaml:"digest"`
	}
	if err := yaml.Unmarshal([]byte("digest:\n"), &doc); err != nil {
		t.Fatal(err)
	}
	digest, err := DecodeDigest(&doc.Digest)
	if err != nil || digest != nil {
		t.Fatalf("DecodeDigest(null) = %v, %v; want nil, nil (bare key counts as absent)", digest, err)
	}
}
