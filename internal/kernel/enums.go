package kernel

// Provider identifies the git host an event originated from. The inbound adapter
// stamps it on every event so handlers, the store, and logs stay
// provider-agnostic.
const (
	ProviderGitHub = "github"
)

// EventKind is the provider-neutral classification of an inbound
// pull-request webhook. Each inbound adapter maps its provider's own vocabulary
// (event names, action strings, review states, draft gating) onto one of these
// kinds; handlers match on the kind alone and never see provider verbs.
//
// The zero value KindUnknown marks a payload no handler acts on — the adapter
// returns it for draft opens, synchronize/label churn, plain-issue comments, and
// anything unmapped, so the dispatcher debug-logs no_handler exactly as before.
type EventKind int

// Recognised event kinds. KindReviewCommented is distinct from KindCommented: a
// submitted review carrying only comments finishes the PR's review session,
// whereas a line/conversation comment (or an edited review) does not.
const (
	KindUnknown EventKind = iota
	KindOpened
	KindReadyForReview
	KindClosed
	KindMerged
	KindConvertedToDraft
	KindApproved
	KindChangesRequested
	KindCommented
	KindReviewCommented
)

// String returns the neutral log token for the kind, used in the ignored-event
// log contract (the kind field).
func (k EventKind) String() string {
	switch k {
	case KindOpened:
		return "opened"
	case KindReadyForReview:
		return "ready_for_review"
	case KindClosed:
		return "closed"
	case KindMerged:
		return "merged"
	case KindConvertedToDraft:
		return "converted_to_draft"
	case KindApproved:
		return "approved"
	case KindChangesRequested:
		return "changes_requested"
	case KindCommented:
		return "commented"
	case KindReviewCommented:
		return "review_commented"
	default:
		return "unknown"
	}
}
