package domain

// BlockTypeSection is the block type the digest emits (a Block Kit "section").
const BlockTypeSection = "section"

// GitHubPRURLPrefix and PullPathSegment build a PR's github.com web URL as
// GitHubPRURLPrefix + "owner/repo" + PullPathSegment + number. The store keeps
// no URL, so it is reconstructed; this assumes github.com (GitHub Enterprise
// hosts are not handled here).
const (
	GitHubPRURLPrefix = "https://github.com/"
	PullPathSegment   = "/pull/"
)
