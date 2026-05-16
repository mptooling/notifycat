// Command notifycat-mapping manages repository → Slack-channel mappings.
// This file owns input parsing/validation and dispatches to the use cases
// in internal/mappingcli.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/mappingcli"
	"github.com/mptooling/notifycat/internal/store"
)

var (
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	channelPattern = regexp.MustCompile(`^[CGD][A-Z0-9]{2,}$`) // Slack channel/IM IDs
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-mapping:", err)
		os.Exit(1)
	}
	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-mapping:", err)
		os.Exit(1)
	}
	defer closeDB(db)

	os.Exit(dispatch(os.Args[1:], db, os.Stdout, os.Stderr, mappingcli.NewProductionValidator(cfg)))
}

func dispatch(args []string, db *gorm.DB, stdout, stderr io.Writer, newValidator mappingcli.ValidatorFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	repo := store.NewRepoMappings(db)
	ctx := context.Background()
	switch args[0] {
	case "add":
		return runAdd(ctx, args[1:], repo, stdout, stderr)
	case "list":
		return mappingcli.List(ctx, repo, stdout, stderr)
	case "remove":
		return runRemove(ctx, args[1:], repo, stdout, stderr)
	case "validate":
		return runValidate(ctx, args[1:], repo, newValidator, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n%s\n", args[0], usage())
		return 2
	}
}

func runAdd(ctx context.Context, args []string, repo *store.RepoMappings, stdout, stderr io.Writer) int {
	mapping, code, ok := parseAddArgs(args, stderr)
	if !ok {
		return code
	}
	return mappingcli.Add(ctx, repo, mapping, stdout, stderr)
}

func parseAddArgs(args []string, stderr io.Writer) (store.RepoMapping, int, bool) {
	if len(args) < 3 {
		fmt.Fprintln(stderr, "usage: add <owner/repo> <channel-id> <comma-separated mentions>")
		return store.RepoMapping{}, 2, false
	}
	repository, channel, rawMentions := args[0], args[1], args[2]
	if !repoPattern.MatchString(repository) {
		fmt.Fprintf(stderr, "invalid repository %q: expected owner/name format\n", repository)
		return store.RepoMapping{}, 2, false
	}
	if !channelPattern.MatchString(channel) {
		fmt.Fprintf(stderr, "invalid channel %q: expected Slack channel ID (e.g. C123ABCDE)\n", channel)
		return store.RepoMapping{}, 2, false
	}
	return store.RepoMapping{
		Repository:   repository,
		SlackChannel: channel,
		Mentions:     splitMentions(rawMentions),
	}, 0, true
}

func runRemove(ctx context.Context, args []string, repo *store.RepoMappings, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: remove <owner/repo>")
		return 2
	}
	return mappingcli.Remove(ctx, repo, args[0], stdout, stderr)
}

func runValidate(
	ctx context.Context,
	args []string,
	repo *store.RepoMappings,
	newValidator mappingcli.ValidatorFactory,
	stdout, stderr io.Writer,
) int {
	target, code, ok := parseValidateArgs(args, stderr)
	if !ok {
		return code
	}
	return mappingcli.Validate(ctx, repo, target, newValidator, stdout, stderr)
}

func parseValidateArgs(args []string, stderr io.Writer) (string, int, bool) {
	switch len(args) {
	case 0:
		return "", 0, true
	case 1:
		return args[0], 0, true
	default:
		fmt.Fprintln(stderr, "usage: validate [owner/repo]")
		return "", 2, false
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

func splitMentions(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := store.SQLDB(db); err == nil {
		_ = sqlDB.Close()
	}
}
