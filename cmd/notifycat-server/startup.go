package main

import (
	"errors"
	"fmt"

	"github.com/mptooling/notifycat/internal/platform/config"
)

// startupError translates known operator configuration mistakes into
// plain-English messages that name the problem and suggest a remedy.
// Genuine internal errors (database failures, network, etc.) pass through unchanged.
func startupError(err error) error {
	var mv *config.MissingVarError
	if errors.As(err, &mv) {
		remedy := mv.Remedy
		if remedy == "" {
			remedy = "copy .env.example to .env and set the missing value"
		}
		return fmt.Errorf("%s is not set — %s", mv.Var, remedy)
	}

	return err
}
