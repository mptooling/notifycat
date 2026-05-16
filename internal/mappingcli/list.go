package mappingcli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/mptooling/notifycat/internal/store"
)

// List prints all mappings in a tab-aligned table.
func List(ctx context.Context, repo *store.RepoMappings, stdout, stderr io.Writer) int {
	rows, err := repo.List(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "list:", err)
		return 1
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tREPOSITORY\tCHANNEL\tMENTIONS")
	for _, m := range rows {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n",
			m.ID, m.Repository, m.SlackChannel, strings.Join(m.Mentions, ","))
	}
	_ = tw.Flush()
	return 0
}
