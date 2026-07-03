package domain

import "time"

// Interval is the fixed cadence between stale-message cleanup passes. 24h is
// conservative enough that a transient DB error has nearly a full day to clear
// before the next attempt.
const Interval = 24 * time.Hour
