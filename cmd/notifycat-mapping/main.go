// Command notifycat-mapping is the CLI for the declarative mappings.yaml
// workflow: `list` prints the file, `validate` runs the cache-aware
// validation pipeline. This file owns argument parsing and dispatches to
// use cases in internal/mappingcli.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/mappingcli"
	"github.com/mptooling/notifycat/internal/mappings"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-mapping:", err)
		os.Exit(1)
	}
	provider, err := mappings.Load(cfg.MappingsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-mapping:", err)
		os.Exit(1)
	}
	validator := mappingcli.NewMappingsValidator(provider, cfg)
	os.Exit(dispatch(os.Args[1:], provider, validator, os.Stdout, os.Stderr))
}

func dispatch(args []string, provider *mappings.Provider, validator mappingcli.MappingsValidator, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	ctx := context.Background()
	switch args[0] {
	case "list":
		return mappingcli.List(provider, stdout)
	case "validate":
		return runValidate(ctx, args[1:], validator, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n%s\n", args[0], usage())
		return 2
	}
}

func runValidate(ctx context.Context, args []string, validator mappingcli.MappingsValidator, stdout, stderr io.Writer) int {
	target, force, code, ok := parseValidateArgs(args, stderr)
	if !ok {
		return code
	}
	return validator.Validate(ctx, target, force, stdout, stderr)
}

func parseValidateArgs(args []string, stderr io.Writer) (target string, force bool, code int, ok bool) {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, "usage: validate [owner/repo] [--force]") }
	forcePtr := fs.Bool("force", false, "ignore the lock file; full revalidation")
	if err := fs.Parse(args); err != nil {
		return "", false, 2, false
	}
	positional := fs.Args()
	switch len(positional) {
	case 0:
		return "", *forcePtr, 0, true
	case 1:
		return positional[0], *forcePtr, 0, true
	default:
		fs.Usage()
		return "", false, 2, false
	}
}

func usage() string {
	return strings.TrimSpace(`
usage:
  notifycat-mapping list
  notifycat-mapping validate [owner/repo] [--force]
`)
}
