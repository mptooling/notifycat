package mappingcli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mptooling/notifycat/internal/store"
)

func cmdRemove(ctx context.Context, args []string, repo *store.RepoMappings, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: remove <owner/repo>")
		return 2
	}
	err := repo.Delete(ctx, args[0])
	if errors.Is(err, store.ErrNotFound) {
		fmt.Fprintf(stderr, "no mapping for %q\n", args[0])
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "remove:", err)
		return 1
	}
	fmt.Fprintf(stdout, "removed %s\n", args[0])
	return 0
}
