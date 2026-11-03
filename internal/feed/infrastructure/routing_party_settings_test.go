package infrastructure_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/feed/infrastructure"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

const (
	partyRepository = "acme/api"
	partyNumber     = 42
)

var repositoryParty = routingdomain.PartySettings{Reviewers: 3, Deadline: 2 * time.Hour}

type fakePartyRules struct {
	hasPathRules     bool
	hasRulesFor      string
	settingsFor      string
	settingsForFiles []string
	settingsCalls    int
}

func (rules *fakePartyRules) HasPartyPathRules(repository string) bool {
	rules.hasRulesFor = repository
	return rules.hasPathRules
}

func (rules *fakePartyRules) PartySettings(repository string, files []string) routingdomain.PartySettings {
	rules.settingsCalls++
	rules.settingsFor = repository
	rules.settingsForFiles = files
	return repositoryParty
}

type fakeChangedFiles struct {
	files    []string
	err      error
	owner    string
	repo     string
	number   int
	calls    int
	deadline time.Time
	hasLimit bool
	wait     bool
}

func (reader *fakeChangedFiles) ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	reader.calls++
	reader.owner, reader.repo, reader.number = owner, repo, number
	reader.deadline, reader.hasLimit = ctx.Deadline()
	if reader.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return reader.files, reader.err
}

func newPartyResolver(rules routingdomain.PartyRules, reader routingdomain.ChangedFilesReader) (*infrastructure.RoutingPartySettings, *bytes.Buffer) {
	logOutput := &bytes.Buffer{}
	return infrastructure.NewRoutingPartySettings(rules, reader, slog.New(slog.NewTextHandler(logOutput, nil))), logOutput
}

func TestRoutingPartySettings_WithoutPartyPathRulesSkipsTheRead(t *testing.T) {
	rules := &fakePartyRules{}
	reader := &fakeChangedFiles{}
	resolver, _ := newPartyResolver(rules, reader)

	resolution := resolver.Resolve(context.Background(), domain.PullRequestKey{Repository: partyRepository, Number: partyNumber})

	assert.Equal(t, domain.PartyResolution{Settings: domain.PartySettings{Reviewers: 3, Deadline: 2 * time.Hour}}, resolution)
	assert.Equal(t, partyRepository, rules.hasRulesFor)
	assert.Equal(t, partyRepository, rules.settingsFor)
	assert.Nil(t, rules.settingsForFiles)
	assert.Zero(t, reader.calls, "no path rule sets a party key, so the files are not needed")
}

func TestRoutingPartySettings_WithoutReaderUsesTheRepositoryValues(t *testing.T) {
	rules := &fakePartyRules{hasPathRules: true}
	resolver, logOutput := newPartyResolver(rules, nil)

	resolution := resolver.Resolve(context.Background(), domain.PullRequestKey{Repository: partyRepository, Number: partyNumber})

	assert.Equal(t, domain.PartyResolution{Settings: domain.PartySettings{Reviewers: 3, Deadline: 2 * time.Hour}}, resolution)
	assert.Equal(t, partyRepository, rules.settingsFor)
	assert.Nil(t, rules.settingsForFiles)
	assert.Empty(t, logOutput.String(), "no read token is an operator choice, not a fault")
}

func TestRoutingPartySettings_ReadsTheChangedFiles(t *testing.T) {
	rules := &fakePartyRules{hasPathRules: true}
	reader := &fakeChangedFiles{files: []string{"billing/x.go"}}
	resolver, _ := newPartyResolver(rules, reader)

	resolution := resolver.Resolve(context.Background(), domain.PullRequestKey{Repository: partyRepository, Number: partyNumber})

	assert.False(t, resolution.Fallback)
	assert.Equal(t, "acme", reader.owner)
	assert.Equal(t, "api", reader.repo)
	assert.Equal(t, partyNumber, reader.number)
	assert.Equal(t, partyRepository, rules.settingsFor)
	assert.Equal(t, []string{"billing/x.go"}, rules.settingsForFiles)
}

func TestRoutingPartySettings_ReadHasItsOwnDeadline(t *testing.T) {
	rules := &fakePartyRules{hasPathRules: true}
	reader := &fakeChangedFiles{}
	resolver, _ := newPartyResolver(rules, reader)

	resolver.Resolve(context.Background(), domain.PullRequestKey{Repository: partyRepository, Number: partyNumber})

	require.True(t, reader.hasLimit)
	assert.WithinDuration(t, time.Now().Add(domain.PartyFilesReadTimeout), reader.deadline, time.Second)
}

func TestRoutingPartySettings_ReadFailureFallsBack(t *testing.T) {
	rules := &fakePartyRules{hasPathRules: true}
	reader := &fakeChangedFiles{files: []string{"ignored.go"}, err: errors.New("github: list files: 502")}
	resolver, logOutput := newPartyResolver(rules, reader)

	resolution := resolver.Resolve(context.Background(), domain.PullRequestKey{Repository: partyRepository, Number: partyNumber})

	assert.Equal(t, domain.PartyResolution{Settings: domain.PartySettings{Reviewers: 3, Deadline: 2 * time.Hour}, Fallback: true}, resolution)
	assert.Equal(t, partyRepository, rules.settingsFor)
	assert.Nil(t, rules.settingsForFiles, "a failed read must not feed partial files to the rules")
	assert.Contains(t, logOutput.String(), "level=WARN")
	assert.Contains(t, logOutput.String(), `msg="feed party settings fallback"`)
	assert.Contains(t, logOutput.String(), "repository=acme/api")
	assert.Contains(t, logOutput.String(), "number=42")
	assert.Contains(t, logOutput.String(), `err="github: list files: 502"`)
}

func TestRoutingPartySettings_SlowReadFallsBack(t *testing.T) {
	rules := &fakePartyRules{hasPathRules: true}
	reader := &fakeChangedFiles{wait: true}
	resolver, logOutput := newPartyResolver(rules, reader)
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	resolution := resolver.Resolve(parent, domain.PullRequestKey{Repository: partyRepository, Number: partyNumber})

	assert.True(t, resolution.Fallback)
	assert.Nil(t, rules.settingsForFiles)
	assert.Contains(t, logOutput.String(), "context deadline exceeded")
}

func TestRoutingPartySettings_MalformedRepositoryFallsBack(t *testing.T) {
	rules := &fakePartyRules{hasPathRules: true}
	reader := &fakeChangedFiles{}
	resolver, logOutput := newPartyResolver(rules, reader)

	resolution := resolver.Resolve(context.Background(), domain.PullRequestKey{Repository: "acme", Number: partyNumber})

	assert.True(t, resolution.Fallback)
	assert.Zero(t, reader.calls)
	assert.Equal(t, "acme", rules.settingsFor)
	assert.Contains(t, logOutput.String(), `msg="feed party settings fallback"`)
	assert.Contains(t, logOutput.String(), "repository=acme")
	assert.Contains(t, logOutput.String(), domain.ErrMalformedRepository.Error())
}
