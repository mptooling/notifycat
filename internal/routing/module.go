// Package routing wires the routing domain — repository→channel resolution
// across tiers and monorepo path rules — into an fx module. This file is the
// only fx-aware part of the domain; the domain, application, and infrastructure
// layers stay framework-free.
package routing

import "go.uber.org/fx"

// Module binds the routing ports to their adapters and use cases. The provider,
// router, and adapter bindings are filled in when the callers are cut over
// (Phase 2, T6); until then the routing packages are proven by their own tests
// and internal/app keeps the live wiring.
var Module = fx.Module("routing")
