// Package kernel holds the shared domain value objects — PR, Message, Event,
// Sender, ReviewState — and the cross-domain enums used by the notification,
// review, and routing domains.
//
// It is the pure center of the hexagon: it imports nothing from other
// internal/... packages (a depguard rule enforces this) and carries no
// persistence, transport, or SDK concerns. Types arrive here as their owning
// domains migrate onto the DDD/fx architecture; notification (Phase 5) is the
// first to populate it.
package kernel
