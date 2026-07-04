package kernel

// GitHubEventType is the value of the X-GitHub-Event header, naming the webhook
// event family.
type GitHubEventType string

// Recognised X-GitHub-Event header values.
const (
	EventPullRequest              GitHubEventType = "pull_request"
	EventPullRequestReview        GitHubEventType = "pull_request_review"
	EventPullRequestReviewComment GitHubEventType = "pull_request_review_comment"
	EventIssueComment             GitHubEventType = "issue_comment"
)

// Action is the webhook payload's action field.
type Action string

// Recognised webhook action values.
const (
	ActionOpened           Action = "opened"
	ActionClosed           Action = "closed"
	ActionReadyForReview   Action = "ready_for_review"
	ActionConvertedToDraft Action = "converted_to_draft"
	ActionSubmitted        Action = "submitted"
	ActionCreated          Action = "created"
	ActionEdited           Action = "edited"
)

// ReviewState is the state of a submitted review.
type ReviewState string

// Recognised review state values.
const (
	ReviewApproved         ReviewState = "approved"
	ReviewCommented        ReviewState = "commented"
	ReviewChangesRequested ReviewState = "changes_requested"
)

// Sender type values. Kept as plain string constants because the AI-suppression
// policy compares the raw sender type string.
const (
	SenderTypeUser = "User"
	SenderTypeBot  = "Bot"
)
