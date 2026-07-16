// Package domain holds the salience domain's contracts: the Advisor port the
// notification and digest consumers inject, the ModelGateway provider port,
// the clamped decision DTOs, enums, and the decision-path constants. The AI
// never composes messages and can never suppress one — the decision schema
// structurally lacks a "don't post" option. Stdlib-only by design; the
// ModelGateway port and its DTOs are deliberately tiny and SDK-free so a
// later promotion to a public pkg/ package is mechanical.
package domain
