package mappings

import (
	"reflect"
	"testing"
)

func TestResolveRouting_RepoOverridesStar(t *testing.T) {
	star := &RepoConfig{Channel: "C0STAR", Mentions: []string{"<@S>"}, MentionsPresent: true}
	repo := &RepoConfig{Channel: "C0REPO"}
	got := resolveRouting(star, repo)
	// channel: repo wins; mentions: repo absent → inherit star's
	if got.Channel != "C0REPO" {
		t.Errorf("Channel = %q; want C0REPO", got.Channel)
	}
	if !reflect.DeepEqual(got.Mentions, []string{"<@S>"}) {
		t.Errorf("Mentions = %v; want star's [<@S>]", got.Mentions)
	}
}

func TestResolveRouting_RepoInheritsChannel(t *testing.T) {
	star := &RepoConfig{Channel: "C0STAR"}
	repo := &RepoConfig{Mentions: []string{"<@U>"}, MentionsPresent: true}
	got := resolveRouting(star, repo)
	if got.Channel != "C0STAR" {
		t.Errorf("Channel = %q; want inherited C0STAR", got.Channel)
	}
	if !reflect.DeepEqual(got.Mentions, []string{"<@U>"}) {
		t.Errorf("Mentions = %v; want repo's", got.Mentions)
	}
}

func TestResolveRouting_NoMentionsAnywhere_DefaultsChannelPing(t *testing.T) {
	got := resolveRouting(nil, &RepoConfig{Channel: "C0REPO"})
	if !reflect.DeepEqual(got.Mentions, []string{ChannelMention}) {
		t.Errorf("Mentions = %v; want [%s]", got.Mentions, ChannelMention)
	}
}

func TestResolveRouting_EmptyMentionsPresent_PingsNobody(t *testing.T) {
	repo := &RepoConfig{Channel: "C0REPO", Mentions: []string{}, MentionsPresent: true}
	got := resolveRouting(nil, repo)
	if len(got.Mentions) != 0 {
		t.Errorf("Mentions = %v; want empty (ping nobody)", got.Mentions)
	}
}

func TestResolveRouting_StarOnly(t *testing.T) {
	got := resolveRouting(&RepoConfig{Channel: "C0STAR"}, nil)
	if got.Channel != "C0STAR" || !reflect.DeepEqual(got.Mentions, []string{ChannelMention}) {
		t.Errorf("got %+v; want channel C0STAR + @channel", got)
	}
}
