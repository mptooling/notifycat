package application_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/digest/application"
	"github.com/mptooling/notifycat/internal/digest/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	salienceapp "github.com/mptooling/notifycat/internal/salience/application"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
)

// stubDigestAdvisor returns one canned digest decision and otherwise behaves
// deterministically.
type stubDigestAdvisor struct {
	*salienceapp.DeterministicAdvisor
	digestDecision *saliencedomain.DigestDecision
	requests       []saliencedomain.DigestDecisionRequest
}

func (s *stubDigestAdvisor) DecideDigest(ctx context.Context, request saliencedomain.DigestDecisionRequest) saliencedomain.DigestDecision {
	s.requests = append(s.requests, request)
	if s.digestDecision != nil {
		return *s.digestDecision
	}
	return s.DeterministicAdvisor.DecideDigest(ctx, request)
}

func TestReporter_AppliesDigestDecision(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	threeDaysAgo := time.Date(2026, 6, 5, 12, 0, 0, 0, time.Local)
	oneDayAgo := time.Date(2026, 6, 7, 12, 0, 0, 0, time.Local)

	finder := fakeFinder{prs: []domain.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: threeDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t1"}}},
		{PRNumber: 51, Repository: "acme/web", UpdatedAt: oneDayAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t2"}}},
	}}
	mappings := fakeMappings{base: routingdomain.RepoMapping{SlackChannel: "C_ACME", Mentions: []string{"<@U1>"}}}
	composer := &fakeComposer{}
	poster := &fakePoster{}
	advisor := &stubDigestAdvisor{
		DeterministicAdvisor: salienceapp.NewDeterministicAdvisor(),
		digestDecision: &saliencedomain.DigestDecision{
			Order:          []int{1, 0}, // newest first — reverses input order
			Highlights:     []saliencedomain.Highlight{saliencedomain.HighlightAttention, saliencedomain.HighlightNormal},
			Notes:          []string{"blocks the release", ""},
			ParentLoudness: saliencedomain.LoudnessQuiet,
		},
	}
	reporter := application.NewReporter(domain.ReporterParams{
		Finder:   finder,
		Mappings: mappings,
		Poster:   poster,
		Composer: composer,
		Digests:  fakeDigestResolver{},
		Advisor:  advisor,
		Logger:   discardLogger(),
		TZ:       time.Local,
		Now:      func() time.Time { return now },
	})

	if err := reporter.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}

	if len(composer.parents) != 1 || composer.parents[0].mentions != nil {
		t.Errorf("parent mentions = %+v; a quiet parent drops them", composer.parents)
	}
	if len(composer.lists) != 1 {
		t.Fatalf("lists rendered = %d; want 1", len(composer.lists))
	}
	rendered := composer.lists[0].prs
	if len(rendered) != 2 || rendered[0].Number != 51 || rendered[1].Number != 42 {
		t.Errorf("list order = %+v; want decision order [51, 42]", rendered)
	}
	if !rendered[1].Attention || rendered[1].Note != "blocks the release" {
		t.Errorf("input index 0 (PR 42) decoration lost: %+v", rendered[1])
	}
	if rendered[0].Attention || rendered[0].Note != "" {
		t.Errorf("input index 1 (PR 51) must stay undecorated: %+v", rendered[0])
	}
	wantRequestPRs := []saliencedomain.DigestPRSummary{
		{Repository: "acme/api", Number: 42, IdleDays: 3},
		{Repository: "acme/web", Number: 51, IdleDays: 1},
	}
	if len(advisor.requests) != 1 || !reflect.DeepEqual(advisor.requests[0].PRs, wantRequestPRs) {
		t.Errorf("advisor request PRs = %+v\nwant %+v", advisor.requests, wantRequestPRs)
	}
}
