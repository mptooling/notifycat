// Package maintenance wires the maintenance domain — stale-message cleanup and
// PR reconcile — into an fx module. This file is the only fx-aware part of the
// domain; the domain, application, and infrastructure layers stay
// framework-free.
package maintenance

import "go.uber.org/fx"

// Module is the fx module for the maintenance domain. It is filled in a later
// task; for now it is an empty option so the package compiles and bindings can
// be added incrementally.
var Module = fx.Options()
