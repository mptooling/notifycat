package domain

import "context"

// Doctor runs the preflight checks and returns one Section per group. A non-empty
// target additionally validates that repository against Slack/GitHub.
type Doctor interface {
	Run(ctx context.Context, target string) []Section
}
