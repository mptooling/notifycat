package digest

import "go.uber.org/fx"

// Module wires the digest domain into an fx module — the scheduled stuck-PR
// reminder. It is the only fx-aware part of the domain; the domain,
// application, and infrastructure layers stay framework-free. Bindings are
// filled in once the layers exist.
var Module = fx.Module("digest")
