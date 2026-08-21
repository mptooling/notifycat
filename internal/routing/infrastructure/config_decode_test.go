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

func decodeOrgErr(body string) error {
	var o map[string]repoConfigWire
	dec := yaml.NewDecoder(strings.NewReader(body))
	dec.KnownFields(true)
	return dec.Decode(&o)
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

func TestRepoConfig_ChannelsList(t *testing.T) {
	o := decodeOrg(t, `
api:
  channels:
    - channel: C0API1
      mentions: ["<@U0ALICE>"]
    - channel: C0API2
`)
	api := o["api"]
	if len(api.Channels) != 2 {
		t.Fatalf("want 2 channels, got %d", len(api.Channels))
	}
	if api.Channels[0].Channel != "C0API1" || !api.Channels[0].MentionsPresent ||
		len(api.Channels[0].Mentions) != 1 || api.Channels[0].Mentions[0] != "<@U0ALICE>" {
		t.Fatalf("entry 0 wrong: %+v", api.Channels[0])
	}
	if api.Channels[1].Channel != "C0API2" || api.Channels[1].MentionsPresent {
		t.Fatalf("entry 1 should have absent mentions: %+v", api.Channels[1])
	}
}

func TestRepoConfig_ChannelsRejectsMixWithChannel(t *testing.T) {
	if decodeOrgErr("api:\n  channel: C0BASE\n  channels:\n    - channel: C0API1\n") == nil {
		t.Fatal("want error mixing channel and channels")
	}
}

func TestRepoConfig_ChannelsRejectsMentionsSibling(t *testing.T) {
	if decodeOrgErr("api:\n  mentions: [\"<@U0ALICE>\"]\n  channels:\n    - channel: C0API1\n") == nil {
		t.Fatal("want error: tier mentions alongside channels")
	}
}

func TestRepoConfig_ChannelsRejectsDuplicate(t *testing.T) {
	if decodeOrgErr("api:\n  channels:\n    - channel: C0DUP\n    - channel: C0DUP\n") == nil {
		t.Fatal("want error: duplicate channel in list")
	}
}

func TestChannelSpec_RejectsMissingChannel(t *testing.T) {
	if decodeOrgErr("api:\n  channels:\n    - mentions: [\"<@U0ALICE>\"]\n") == nil {
		t.Fatal("want error: entry missing channel")
	}
}

func TestRepoConfig_ChannelsRejectsEmptyList(t *testing.T) {
	if decodeOrgErr("api:\n  channels: []\n") == nil {
		t.Fatal("want error: empty channels list")
	}
}

func TestPathRule_ChannelsList(t *testing.T) {
	o := decodeOrg(t, `
monorepo:
  channel: C0BASE
  paths:
    services/pay:
      channels:
        - channel: C0PAY1
        - channel: C0PAY2
          mentions: []
`)
	rule := o["monorepo"].Paths[0]
	if rule.Dir != "services/pay" || len(rule.Channels) != 2 || rule.Channels[0].Channel != "C0PAY1" {
		t.Fatalf("want 2 path channels under services/pay, got %+v", rule)
	}
	if rule.Channels[1].Channel != "C0PAY2" || !rule.Channels[1].MentionsPresent || len(rule.Channels[1].Mentions) != 0 {
		t.Fatalf("entry 1 should be explicit-empty mentions: %+v", rule.Channels[1])
	}
}

func TestPathRule_ChannelsRejectsMixWithChannel(t *testing.T) {
	if decodeOrgErr("monorepo:\n  channel: C0BASE\n  paths:\n    services/pay:\n      channel: C0PAY\n      channels:\n        - channel: C0PAY1\n") == nil {
		t.Fatal("want error mixing path channel and channels")
	}
}

func TestPathRule_ChannelsRejectsMixWithMentions(t *testing.T) {
	if decodeOrgErr("monorepo:\n  channel: C0BASE\n  paths:\n    services/pay:\n      mentions: [\"<@U0ALICE>\"]\n      channels:\n        - channel: C0PAY1\n") == nil {
		t.Fatal("want error mixing path mentions and channels")
func TestRepoConfig_DigestCountryRejected(t *testing.T) {
	// country is global-only for the same reason as timezone: one team calendar
	// per server. Setting it on a tier must fail, not be silently ignored.
	var o map[string]repoConfigWire
	dec := yaml.NewDecoder(strings.NewReader("api:\n  channel: C0API\n  digest:\n    country: US\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err == nil {
		t.Fatal("expected error for per-repo digest.country")
	}
}
