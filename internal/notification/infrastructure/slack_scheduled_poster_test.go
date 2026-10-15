package infrastructure

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/slack"
)

func TestSlackScheduledPoster_ComposeList(t *testing.T) {
	composer := slack.NewComposer("eyes", false)
	poster := NewSlackScheduledPoster(slack.NewClient(http.DefaultClient, "xoxb-test"), composer)
	pullRequest := samplePR()

	message := poster.composeList(domain.ScheduledListRequest{
		Mentions: []string{"<!channel>"},
		Entries:  []domain.ScheduledEntry{{Repository: "acme/api", PR: pullRequest, Channel: "C_ACME"}},
	})

	want := composer.ScheduledList([]string{"<!channel>"}, []slack.PRDetails{prDetails("acme/api", pullRequest)})
	assert.Equal(t, want, message)
}
