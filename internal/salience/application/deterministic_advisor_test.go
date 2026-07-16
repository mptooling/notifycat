package application_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/mptooling/notifycat/internal/salience/application"
	"github.com/mptooling/notifycat/internal/salience/domain"
)

func TestDeterministicAdvisorDecideOpen(t *testing.T) {
	advisor := application.NewDeterministicAdvisor()
	request := domain.OpenDecisionRequest{
		Repository: "acme/api",
		Candidates: []domain.CandidateTarget{
			{Channel: "C0000000001", Mentions: []string{"<@U1>", "<@U2>"}},
			{Channel: "C0000000002"},
		},
		DefaultEmoji: "eyes",
	}

	decision := advisor.DecideOpen(context.Background(), request)

	want := []domain.TargetDecision{
		{Channel: "C0000000001", Loudness: domain.LoudnessPing, Mentions: []string{"<@U1>", "<@U2>"}, LeadingEmoji: "eyes", Format: domain.FormatStandard, Emphasis: domain.EmphasisNone},
		{Channel: "C0000000002", Loudness: domain.LoudnessPing, LeadingEmoji: "eyes", Format: domain.FormatStandard, Emphasis: domain.EmphasisNone},
	}
	if !reflect.DeepEqual(decision.Targets, want) {
		t.Errorf("Targets = %+v\nwant %+v", decision.Targets, want)
	}
	if decision.FallbackReason != domain.FallbackNone {
		t.Errorf("FallbackReason = %q; want empty", decision.FallbackReason)
	}
}

func TestDeterministicAdvisorDecideUpdated(t *testing.T) {
	advisor := application.NewDeterministicAdvisor()
	decision := advisor.DecideUpdated(context.Background(), domain.UpdatedDecisionRequest{DefaultEmoji: "white_check_mark"})
	if decision.Emoji != "white_check_mark" {
		t.Errorf("Emoji = %q; want the configured default", decision.Emoji)
	}
}

func TestDeterministicAdvisorDecideDigest(t *testing.T) {
	advisor := application.NewDeterministicAdvisor()
	request := domain.DigestDecisionRequest{
		Channel: "C0000000001",
		PRs: []domain.DigestPRSummary{
			{Repository: "acme/api", Number: 1, IdleDays: 3},
			{Repository: "acme/web", Number: 9, IdleDays: 1},
		},
	}

	decision := advisor.DecideDigest(context.Background(), request)

	if !reflect.DeepEqual(decision.Order, []int{0, 1}) {
		t.Errorf("Order = %v; want identity", decision.Order)
	}
	if !reflect.DeepEqual(decision.Highlights, []domain.Highlight{domain.HighlightNormal, domain.HighlightNormal}) {
		t.Errorf("Highlights = %v; want all normal", decision.Highlights)
	}
	if !reflect.DeepEqual(decision.Notes, []string{"", ""}) {
		t.Errorf("Notes = %v; want all empty", decision.Notes)
	}
	if decision.ParentLoudness != domain.LoudnessPing {
		t.Errorf("ParentLoudness = %q; want ping", decision.ParentLoudness)
	}
}
