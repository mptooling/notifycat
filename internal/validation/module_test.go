package validation_test

import (
	"context"
	"net/http"
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/validation"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

type stubMappingLookup struct{}

func (stubMappingLookup) Get(_ context.Context, _ string) (routingdomain.RepoMapping, error) {
	return routingdomain.RepoMapping{}, routingdomain.ErrNotFound
}

func (stubMappingLookup) PathChannels(_ string) []string { return nil }

type stubGitHubChecker struct{}

func (stubGitHubChecker) ListHookEvents(_ context.Context, _, _, _ string) ([]string, error) {
	return nil, nil
}

// TestModule_BuildsGraph proves the validation fx module wires its ports:
// given a *slack.Client and the two externally supplied ports, fx can
// construct a domain.RepoValidator (the Validator over the Slack probe).
func TestModule_BuildsGraph(t *testing.T) {
	app := fxtest.New(t,
		fx.Supply(slack.NewClient(http.DefaultClient, "xoxb-test")),
		fx.Provide(
			func() domain.MappingLookup { return stubMappingLookup{} },
			func() domain.GitHubChecker { return stubGitHubChecker{} },
		),
		validation.Module,
		fx.Invoke(func(domain.RepoValidator) {}),
	)
	app.RequireStart().RequireStop()
}
