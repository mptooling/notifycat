package mappingcli

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/mptooling/notifycat/internal/store"
)

var (
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	channelPattern = regexp.MustCompile(`^[CGD][A-Z0-9]{2,}$`) // Slack channel/IM IDs
)

func cmdAdd(ctx context.Context, args []string, repo *store.RepoMappings, stdout, stderr io.Writer) int {
	if len(args) < 3 {
		fmt.Fprintln(stderr, "usage: add <owner/repo> <channel-id> <comma-separated mentions>")
		return 2
	}
	repository, channel, rawMentions := args[0], args[1], args[2]
	if code, ok := validateAddArgs(repository, channel, stderr); !ok {
		return code
	}
	out, err := repo.Upsert(ctx, store.RepoMapping{
		Repository:   repository,
		SlackChannel: channel,
		Mentions:     splitMentions(rawMentions),
	})
	if err != nil {
		fmt.Fprintln(stderr, "upsert:", err)
		return 1
	}
	fmt.Fprintf(stdout, "saved %s → %s (id=%d, mentions=%d)\n",
		out.Repository, out.SlackChannel, out.ID, len(out.Mentions))
	return 0
}

func validateAddArgs(repository, channel string, stderr io.Writer) (int, bool) {
	if !repoPattern.MatchString(repository) {
		fmt.Fprintf(stderr, "invalid repository %q: expected owner/name format\n", repository)
		return 2, false
	}
	if !channelPattern.MatchString(channel) {
		fmt.Fprintf(stderr, "invalid channel %q: expected Slack channel ID (e.g. C123ABCDE)\n", channel)
		return 2, false
	}
	return 0, true
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
