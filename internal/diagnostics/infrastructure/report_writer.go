package infrastructure

import (
	"fmt"
	"io"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// WriteReport renders sections to w in a human-readable, greppable form, and
// returns true iff no check failed (skipped checks do not fail the report).
// The format is intentionally plain text: one section header per group, then
// one indented line per check.
func WriteReport(w io.Writer, sections []diagnosticsdomain.Section) bool {
	allOK := true
	for _, sec := range sections {
		fmt.Fprintf(w, "[%s]\n", sec.Name)
		for _, c := range sec.Checks {
			if c.Status == validationdomain.StatusFail {
				allOK = false
			}
			if c.Detail == "" {
				fmt.Fprintf(w, "  %-4s  %s\n", c.Status, c.Name)
				continue
			}
			fmt.Fprintf(w, "  %-4s  %s — %s\n", c.Status, c.Name, c.Detail)
		}
	}
	return allOK
}
