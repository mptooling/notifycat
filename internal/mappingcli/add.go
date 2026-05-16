package mappingcli

import (
	"context"
	"fmt"
	"io"

	"github.com/mptooling/notifycat/internal/store"
)

// Add upserts a single mapping. The caller is responsible for validating
// input shape (regex, arg counts) — this is the use case, not the parser.
func Add(ctx context.Context, repo *store.RepoMappings, mapping store.RepoMapping, stdout, stderr io.Writer) int {
	out, err := repo.Upsert(ctx, mapping)
	if err != nil {
		fmt.Fprintln(stderr, "upsert:", err)
		return 1
	}
	fmt.Fprintf(stdout, "saved %s → %s (id=%d, mentions=%d)\n",
		out.Repository, out.SlackChannel, out.ID, len(out.Mentions))
	return 0
}
