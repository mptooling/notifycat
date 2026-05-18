package mappingcli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/mptooling/notifycat/internal/mappings"
)

// List prints provider entries in a tab-aligned table, in the deterministic
// order Entries() returns (org A→Z, then repo A→Z; wildcards render as "*").
// No network calls.
func List(provider *mappings.Provider, stdout io.Writer) int {
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ORG\tREPO\tCHANNEL\tMENTIONS")
	for _, e := range provider.Entries() {
		repo := e.Repo
		if e.Wildcard {
			repo = "*"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			e.Org, repo, e.Channel, strings.Join(e.Mentions, ","))
	}
	_ = tw.Flush()
	return 0
}
