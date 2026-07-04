package application

import (
	"fmt"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

func mappingFoundCheck(m routingdomain.RepoMapping) domain.CheckResult {
	return domain.CheckResult{
		Name:   "mapping",
		Status: domain.StatusOK,
		Detail: fmt.Sprintf("found mapping → %s", m.SlackChannel),
	}
}

// channelFormatCheck returns the check result and a boolean indicating whether
// the channel format is acceptable. When false, callers should skip downstream
// Slack probes since they would just emit channel_not_found noise.
func channelFormatCheck(m routingdomain.RepoMapping) (domain.CheckResult, bool) {
	if !domain.ChannelIDPattern.MatchString(m.SlackChannel) {
		return domain.CheckResult{
			Name:   "channel-format",
			Status: domain.StatusFail,
			Detail: fmt.Sprintf("channel id %q is not a valid Slack ID (expected C…/G…/D…); correct it in the mappings: section of config.yaml (use `notifycat-config` to inspect the current values)", m.SlackChannel),
		}, false
	}
	return domain.CheckResult{
		Name:   "channel-format",
		Status: domain.StatusOK,
		Detail: fmt.Sprintf("channel id %s is well-formed", m.SlackChannel),
	}, true
}
