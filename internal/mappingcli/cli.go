// Package mappingcli implements the `notifycat-mapping` command. The cmd
// binary delegates here so its main() stays a thin entrypoint.
package mappingcli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/store"
)

// Run is the public entrypoint: it wires up the production validator
// against cfg and dispatches the subcommand. It returns the exit code the
// binary should use.
func Run(args []string, db *gorm.DB, stdout, stderr io.Writer, cfg config.Config) int {
	return run(args, db, stdout, stderr, newProductionValidator(cfg))
}

// run is the package-internal dispatcher. Tests inject a fake
// ValidatorFactory through this seam without touching real clients.
func run(args []string, db *gorm.DB, stdout, stderr io.Writer, newValidator ValidatorFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	repo := store.NewRepoMappings(db)
	ctx := context.Background()
	switch args[0] {
	case "add":
		return cmdAdd(ctx, args[1:], repo, stdout, stderr)
	case "list":
		return cmdList(ctx, repo, stdout, stderr)
	case "remove":
		return cmdRemove(ctx, args[1:], repo, stdout, stderr)
	case "validate":
		return cmdValidate(ctx, args[1:], repo, newValidator, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n%s\n", args[0], usage())
		return 2
	}
}

func usage() string {
	return strings.TrimSpace(`
usage:
  notifycat-mapping add <owner/repo> <channel-id> <comma-separated mentions>
  notifycat-mapping list
  notifycat-mapping remove <owner/repo>
  notifycat-mapping validate [owner/repo]
`)
}
