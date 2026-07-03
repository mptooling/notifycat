// Package domain holds the routing domain's contracts: the ports, DTOs, enums,
// constants, and pure validation for resolving a repository (and a PR's changed
// files) to the Slack channel(s) and behavioural config that apply, across the
// global/org/repo tiers and monorepo path rules.
//
// It depends only on the standard library, the shared kernel, and the YAML
// codec used to decode the mappings section of config.yaml onto these types
// (transition debt: the wire-decoding lives on the domain types until a later
// phase splits wire types from domain types). It never imports application,
// infrastructure, or a platform client (store/slack/github).
package domain
