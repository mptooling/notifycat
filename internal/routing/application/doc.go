// Package application holds the routing use cases: the provider (tier
// resolution and defaults merge) and the per-PR router (layering monorepo path
// rules over the base repo/org tier). It depends only on the routing domain
// layer and the standard library — never on infrastructure or a platform
// client.
package application
