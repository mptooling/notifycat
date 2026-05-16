package validate

import (
	"fmt"

	"github.com/mptooling/notifycat/internal/store"
)

func mappingFoundCheck(m store.RepoMapping) CheckResult {
	return CheckResult{
		Name:   "mapping",
		Status: StatusOK,
		Detail: fmt.Sprintf("found mapping → %s", m.SlackChannel),
	}
}

// channelFormatCheck returns the check result and a boolean indicating
// whether the channel format is acceptable. When false, callers should
// skip downstream Slack probes since they would just emit channel_not_found
// noise.
func channelFormatCheck(m store.RepoMapping) (CheckResult, bool) {
	if !channelIDPattern.MatchString(m.SlackChannel) {
		return CheckResult{
			Name:   "channel-format",
			Status: StatusFail,
			Detail: fmt.Sprintf("channel id %q is not a valid Slack ID (expected C…/G…/D…); rewrite with `notifycat-mapping add %s <channel-id> <mentions>`", m.SlackChannel, m.Repository),
		}, false
	}
	return CheckResult{
		Name:   "channel-format",
		Status: StatusOK,
		Detail: fmt.Sprintf("channel id %s is well-formed", m.SlackChannel),
	}, true
}
