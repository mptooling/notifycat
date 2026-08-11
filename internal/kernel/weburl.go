package kernel

import "strconv"

// Web URL hosts and path segments used to reconstruct a pull request's browser
// URL from its "owner/repo" (or "workspace/repo") slug and number. github.com
// and bitbucket.org only; self-hosted / Enterprise hosts are not handled.
const (
	gitHubWebHost      = "https://github.com/"
	gitHubPullSegment  = "/pull/"
	bitbucketWebHost   = "https://bitbucket.org/"
	bitbucketPRSegment = "/pull-requests/"
)

// PullRequestWebURL builds the browser URL for a pull request from its repository
// slug and number. The store keeps no URL, so callers that hold only repo+number
// (the digest reminder, the reconciler's log lines) reconstruct it here. An
// unknown or zero-value provider falls back to the github.com form.
func (p Provider) PullRequestWebURL(repository string, number int) string {
	switch p {
	case ProviderBitbucket:
		return bitbucketWebHost + repository + bitbucketPRSegment + strconv.Itoa(number)
	default:
		return gitHubWebHost + repository + gitHubPullSegment + strconv.Itoa(number)
	}
}
