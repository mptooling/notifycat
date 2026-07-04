package diagnostics

import "go.uber.org/fx"

// Module wires the diagnostics domain into an fx module — the operator tooling.
// It is the only fx-aware part of the domain. Bindings are filled in once the
// layers exist.
var Module = fx.Module("diagnostics")
