package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/mappings"
)

// startupError translates known operator configuration mistakes into
// plain-English messages that name the problem and suggest a remedy.
// Genuine internal errors (database failures, network, etc.) pass through unchanged.
func startupError(err error) error {
	var mv *config.MissingVarError
	if errors.As(err, &mv) {
		return fmt.Errorf("%s is not set — copy .env.example to .env and set the missing value", mv.Var)
	}

	var nfe *mappings.FileNotFoundError
	if errors.As(err, &nfe) {
		switch {
		case errors.Is(nfe.Err, os.ErrNotExist):
			return fmt.Errorf("mappings file not found at %s — copy mappings.example.yaml to get started", nfe.Path)
		case errors.Is(nfe.Err, os.ErrPermission):
			return fmt.Errorf("mappings file at %s is not readable — check file permissions (container user is 65532)", nfe.Path)
		default:
			return fmt.Errorf("mappings file at %s could not be opened: %s", nfe.Path, nfe.Err)
		}
	}

	var pe *mappings.ParseError
	if errors.As(err, &pe) {
		msg := pe.Err.Error()
		// Strip the internal "mappings: parse: " prefix so the parser detail
		// reads naturally in a user-facing message.
		if after, ok := strings.CutPrefix(msg, "mappings: parse: "); ok {
			msg = after
		}
		return fmt.Errorf("mappings file at %s is invalid — %s; run notifycat-doctor for details", pe.Path, msg)
	}

	return err
}
