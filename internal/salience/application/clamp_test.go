package application

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

func clampOpenRequest() domain.OpenDecisionRequest {
	return domain.OpenDecisionRequest{
		Repository: "acme/api",
		Candidates: []domain.CandidateTarget{
			{Channel: "C0000000001", Mentions: []string{"<@U1>", "<@U2>"}},
			{Channel: "C0000000002", Mentions: []string{"<@U3>"}},
		},
		DefaultEmoji:   "eyes",
		EmojiAllowlist: []string{"eyes", "rocket", "warning"},
	}
}

func TestClampOpenDropsUnknownChannels(t *testing.T) {
	// Model returns C0000000001 (valid, rocket emoji) + unknown C9999999999.
	// Unknown is dropped; C0000000002 (not returned by model) is padded
	// deterministically with "eyes". Result must have exactly 2 targets.
	decision := domain.OpenDecision{Targets: []domain.TargetDecision{
		{Channel: "C0000000001", Loudness: domain.LoudnessPing, Mentions: []string{"<@U1>"}, LeadingEmoji: "rocket", Format: domain.FormatStandard, Emphasis: domain.EmphasisNone},
		{Channel: "C9999999999", Loudness: domain.LoudnessPing, LeadingEmoji: "eyes", Format: domain.FormatStandard, Emphasis: domain.EmphasisNone},
	}}
	clamped, violated := clampOpen(decision, clampOpenRequest())
	if !violated {
		t.Error("unknown channel must flag a violation")
	}
	if len(clamped.Targets) != 2 {
		t.Fatalf("Targets = %d; want 2 (unknown dropped, omitted candidate padded)", len(clamped.Targets))
	}
	// C0000000001 is first candidate — keeps the model's rocket emoji.
	if clamped.Targets[0].Channel != "C0000000001" || clamped.Targets[0].LeadingEmoji != "rocket" {
		t.Errorf("Targets[0] = %+v; want C0000000001 with rocket emoji", clamped.Targets[0])
	}
	// C0000000002 was omitted by model — padded deterministically with "eyes".
	if clamped.Targets[1].Channel != "C0000000002" || clamped.Targets[1].LeadingEmoji != "eyes" || clamped.Targets[1].Loudness != domain.LoudnessPing {
		t.Errorf("Targets[1] = %+v; want C0000000002 padded deterministically (eyes, ping)", clamped.Targets[1])
	}
	// The unknown channel must not appear anywhere.
	for _, target := range clamped.Targets {
		if target.Channel == "C9999999999" {
			t.Error("unknown channel C9999999999 must not appear in result")
		}
	}
}

func TestClampOpenEmptyTargetsFallsBackToAllCandidates(t *testing.T) {
	clamped, violated := clampOpen(domain.OpenDecision{}, clampOpenRequest())
	if !violated {
		t.Error("empty target list must flag a violation")
	}
	if len(clamped.Targets) != 2 {
		t.Fatalf("Targets = %d; never-skip means all candidates post", len(clamped.Targets))
	}
	if clamped.Targets[0].LeadingEmoji != "eyes" || clamped.Targets[0].Loudness != domain.LoudnessPing {
		t.Errorf("fallback target not deterministic: %+v", clamped.Targets[0])
	}
}

func TestClampOpenRepairsInvalidFieldsPerChannel(t *testing.T) {
	decision := domain.OpenDecision{Targets: []domain.TargetDecision{{
		Channel:      "C0000000001",
		Loudness:     "shout",                       // invalid enum
		Mentions:     []string{"<@U1>", "<@UEVIL>"}, // not a subset
		LeadingEmoji: "smiling_imp",                 // not allowlisted
		Format:       domain.FormatCompact,          // valid — must survive
		Emphasis:     "sirens",                      // invalid enum
		ContextBlock: "ping <@U9> https://evil.example now " + strings.Repeat("x", 300),
	}}}
	clamped, violated := clampOpen(decision, clampOpenRequest())
	if !violated {
		t.Error("violations must be flagged")
	}
	target := clamped.Targets[0]
	if target.Loudness != domain.LoudnessPing {
		t.Errorf("Loudness = %q; invalid enum repairs to ping", target.Loudness)
	}
	if !reflect.DeepEqual(target.Mentions, []string{"<@U1>", "<@U2>"}) {
		t.Errorf("Mentions = %v; non-subset repairs to the configured set", target.Mentions)
	}
	if target.LeadingEmoji != "eyes" {
		t.Errorf("LeadingEmoji = %q; off-allowlist repairs to the default", target.LeadingEmoji)
	}
	if target.Format != domain.FormatCompact {
		t.Errorf("Format = %q; valid fields must survive a sibling violation", target.Format)
	}
	if len([]rune(target.ContextBlock)) > domain.MaxContextBlockChars || strings.Contains(target.ContextBlock, "<@") || strings.Contains(target.ContextBlock, "https://") {
		t.Errorf("ContextBlock unsafe: %q", target.ContextBlock)
	}
}

func TestClampOpenOmittedChannelIsPaddedDeterministically(t *testing.T) {
	// Model returns ONLY C0000000002 (quiet, warning, breaking, context note).
	// C0000000001 is omitted — must be padded deterministically at index 0.
	// Result: violated=true, len==2, Targets[0] is C0000000001 padded (ping/eyes),
	// Targets[1] is C0000000002 with the model's values preserved.
	decision := domain.OpenDecision{Targets: []domain.TargetDecision{{
		Channel: "C0000000002", Loudness: domain.LoudnessQuiet, Mentions: []string{},
		LeadingEmoji: "warning", Format: domain.FormatStandard, Emphasis: domain.EmphasisBreaking,
		ContextBlock: "touches shared billing types",
	}}}
	clamped, violated := clampOpen(decision, clampOpenRequest())
	if !violated {
		t.Error("an omitted candidate channel must flag a violation")
	}
	if len(clamped.Targets) != 2 {
		t.Fatalf("Targets = %d; want 2 (all candidates must be present)", len(clamped.Targets))
	}
	// Targets[0] is C0000000001 — omitted by model, padded deterministically.
	padded := clamped.Targets[0]
	if padded.Channel != "C0000000001" {
		t.Errorf("Targets[0].Channel = %q; want C0000000001 (padded)", padded.Channel)
	}
	if padded.LeadingEmoji != "eyes" || padded.Loudness != domain.LoudnessPing {
		t.Errorf("Targets[0] not deterministic: LeadingEmoji=%q Loudness=%q; want eyes/ping", padded.LeadingEmoji, padded.Loudness)
	}
	// Targets[1] is C0000000002 — model's values must be preserved.
	kept := clamped.Targets[1]
	if kept.Channel != "C0000000002" {
		t.Errorf("Targets[1].Channel = %q; want C0000000002", kept.Channel)
	}
	if kept.Loudness != domain.LoudnessQuiet || kept.LeadingEmoji != "warning" || kept.Emphasis != domain.EmphasisBreaking {
		t.Errorf("Targets[1] model values not preserved: %+v", kept)
	}
	if kept.ContextBlock != "touches shared billing types" {
		t.Errorf("Targets[1].ContextBlock = %q; want model's value", kept.ContextBlock)
	}
}

func TestClampOpenAllCandidatesPresentPasses(t *testing.T) {
	// Happy path: model returns valid decisions for both candidates.
	// No violation, both model emojis preserved.
	decision := domain.OpenDecision{Targets: []domain.TargetDecision{
		{
			Channel: "C0000000001", Loudness: domain.LoudnessPing, Mentions: []string{"<@U1>"},
			LeadingEmoji: "rocket", Format: domain.FormatStandard, Emphasis: domain.EmphasisNone,
		},
		{
			Channel: "C0000000002", Loudness: domain.LoudnessQuiet, Mentions: []string{},
			LeadingEmoji: "warning", Format: domain.FormatCompact, Emphasis: domain.EmphasisBreaking,
		},
	}}
	clamped, violated := clampOpen(decision, clampOpenRequest())
	if violated {
		t.Error("fully valid decision covering all candidates must not flag a violation")
	}
	if len(clamped.Targets) != 2 {
		t.Fatalf("Targets = %d; want 2", len(clamped.Targets))
	}
	if clamped.Targets[0].LeadingEmoji != "rocket" {
		t.Errorf("Targets[0].LeadingEmoji = %q; want rocket (model's choice)", clamped.Targets[0].LeadingEmoji)
	}
	if clamped.Targets[1].LeadingEmoji != "warning" {
		t.Errorf("Targets[1].LeadingEmoji = %q; want warning (model's choice)", clamped.Targets[1].LeadingEmoji)
	}
	if clamped.Targets[1].Format != domain.FormatCompact {
		t.Errorf("Targets[1].Format = %q; want compact (model's choice)", clamped.Targets[1].Format)
	}
	if clamped.Targets[1].Emphasis != domain.EmphasisBreaking {
		t.Errorf("Targets[1].Emphasis = %q; want breaking (model's choice)", clamped.Targets[1].Emphasis)
	}
}

func TestClampUpdated(t *testing.T) {
	request := domain.UpdatedDecisionRequest{DefaultEmoji: "x", EmojiAllowlist: []string{"x", "rocket"}}
	if decision, violated := clampUpdated(domain.UpdatedDecision{Emoji: "rocket"}, request); violated || decision.Emoji != "rocket" {
		t.Errorf("valid emoji clamped: %+v violated=%v", decision, violated)
	}
	if decision, violated := clampUpdated(domain.UpdatedDecision{Emoji: "smiling_imp"}, request); !violated || decision.Emoji != "x" {
		t.Errorf("invalid emoji not repaired: %+v violated=%v", decision, violated)
	}
	if decision, violated := clampUpdated(domain.UpdatedDecision{}, request); violated || decision.Emoji != "x" {
		t.Errorf("empty emoji must repair to default without violation: %+v violated=%v", decision, violated)
	}
}

func TestClampDigestInvalidPermutationFallsBack(t *testing.T) {
	request := domain.DigestDecisionRequest{PRs: []domain.DigestPRSummary{{Number: 1}, {Number: 2}, {Number: 3}}}
	decision := domain.DigestDecision{
		Order:          []int{0, 0, 2}, // not a permutation
		Highlights:     []domain.Highlight{domain.HighlightNormal, domain.HighlightNormal, domain.HighlightNormal},
		Notes:          []string{"", "", ""},
		ParentLoudness: domain.LoudnessPing,
	}
	clamped, violated := clampDigest(decision, request)
	if !violated {
		t.Error("invalid permutation must flag a violation")
	}
	if !reflect.DeepEqual(clamped.Order, []int{0, 1, 2}) {
		t.Errorf("Order = %v; want deterministic identity", clamped.Order)
	}
}

func TestClampDigestSanitizesNotes(t *testing.T) {
	request := domain.DigestDecisionRequest{PRs: []domain.DigestPRSummary{{Number: 1}}}
	decision := domain.DigestDecision{
		Order:          []int{0},
		Highlights:     []domain.Highlight{domain.HighlightAttention},
		Notes:          []string{"<@U1> " + strings.Repeat("z", 300)},
		ParentLoudness: domain.LoudnessQuiet,
	}
	clamped, _ := clampDigest(decision, request)
	if len([]rune(clamped.Notes[0])) > domain.MaxDigestNoteChars || strings.Contains(clamped.Notes[0], "<@") {
		t.Errorf("note unsafe: %q", clamped.Notes[0])
	}
	if clamped.ParentLoudness != domain.LoudnessQuiet || clamped.Highlights[0] != domain.HighlightAttention {
		t.Errorf("valid enums mutated: %+v", clamped)
	}
}
