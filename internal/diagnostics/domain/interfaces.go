package domain

import "context"

// Doctor runs the preflight checks and returns one Section per group. A non-empty
// target additionally validates that repository against Slack/GitHub.
type Doctor interface {
	Run(ctx context.Context, target string) []Section
}

// AIProber performs the live AI provider probe. Nil-able dependency: doctor
// skips the live checks (reporting config shape only) when no prober is
// wired or the feature is disabled.
type AIProber interface {
	Probe(ctx context.Context) AIProbeResult
}
