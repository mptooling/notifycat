package review

import "go.uber.org/fx"

// Module wires the review domain into an fx module — the interactive
// "Start review" flow. It is the only fx-aware part of the domain. Bindings are
// filled in once the layers exist.
var Module = fx.Module("review")
