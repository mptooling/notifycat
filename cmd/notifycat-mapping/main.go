// Command notifycat-mapping manages repository → Slack-channel mappings.
//
// Subcommands:
//
//	notifycat-mapping add <owner/repo> <channel-id> <comma-separated mentions>
//	notifycat-mapping list
//	notifycat-mapping remove <owner/repo>
//
// DATABASE_URL controls the target database (default file:./data/notifycat.db).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"

	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/config"
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
	defer func() {
		if sqlDB, err := store.SQLDB(db); err == nil {
			_ = sqlDB.Close()
		}
	}()

	os.Exit(run(os.Args[1:], db, os.Stdout, os.Stderr))
}

// run is the testable entry point. It writes user-facing output to stdout and
// errors to stderr, and returns a process exit code.
func run(args []string, db *gorm.DB, stdout, stderr io.Writer) int {
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
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n%s\n", args[0], usage())
		return 2
	}
}

func cmdAdd(ctx context.Context, args []string, repo *store.RepoMappings, stdout, stderr io.Writer) int {
	if len(args) < 3 {
		fmt.Fprintln(stderr, "usage: add <owner/repo> <channel-id> <comma-separated mentions>")
		return 2
	}
	repository, channel, rawMentions := args[0], args[1], args[2]

	if !repoPattern.MatchString(repository) {
		fmt.Fprintf(stderr, "invalid repository %q: expected owner/name format\n", repository)
		return 2
	}
	if !channelPattern.MatchString(channel) {
		fmt.Fprintf(stderr, "invalid channel %q: expected Slack channel ID (e.g. C123ABCDE)\n", channel)
		return 2
	}

	mentions := splitMentions(rawMentions)
	out, err := repo.Upsert(ctx, store.RepoMapping{
		Repository:   repository,
		SlackChannel: channel,
		Mentions:     mentions,
	})
	if err != nil {
		fmt.Fprintln(stderr, "upsert:", err)
		return 1
	}
	fmt.Fprintf(stdout, "saved %s → %s (id=%d, mentions=%d)\n", out.Repository, out.SlackChannel, out.ID, len(out.Mentions))
	return 0
}

func cmdList(ctx context.Context, repo *store.RepoMappings, stdout, stderr io.Writer) int {
	rows, err := repo.List(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "list:", err)
		return 1
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tREPOSITORY\tCHANNEL\tMENTIONS")
	for _, m := range rows {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", m.ID, m.Repository, m.SlackChannel, strings.Join(m.Mentions, ","))
	}
	_ = tw.Flush()
	return 0
}

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

func usage() string {
	return strings.TrimSpace(`
usage:
  notifycat-mapping add <owner/repo> <channel-id> <comma-separated mentions>
  notifycat-mapping list
  notifycat-mapping remove <owner/repo>
`)
}
