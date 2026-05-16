package mappingcli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mptooling/notifycat/internal/store"
)

// Remove deletes a mapping by repository name.
func Remove(ctx context.Context, repo *store.RepoMappings, repository string, stdout, stderr io.Writer) int {
	err := repo.Delete(ctx, repository)
	if errors.Is(err, store.ErrNotFound) {
		fmt.Fprintf(stderr, "no mapping for %q\n", repository)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "remove:", err)
		return 1
	}
	fmt.Fprintf(stdout, "removed %s\n", repository)
	return 0
}
