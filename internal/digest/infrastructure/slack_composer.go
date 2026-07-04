package infrastructure

import (
	"github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/platform/slack"
)

// SlackComposer adapts the platform Slack composer to the digest's
// DigestComposer port, mapping the Slack message type to the domain's
// presentation-neutral Message at the boundary.
type SlackComposer struct {
	composer *slack.Composer
}

// NewSlackComposer wraps the platform Slack composer.
func NewSlackComposer(composer *slack.Composer) *SlackComposer {
	return &SlackComposer{composer: composer}
}

// StuckDigestParent implements domain.DigestComposer.
func (c *SlackComposer) StuckDigestParent(mentions []string, count int) domain.Message {
	return toDomainMessage(c.composer.StuckDigestParent(mentions, count))
}

// StuckDigestList implements domain.DigestComposer.
func (c *SlackComposer) StuckDigestList(prs []domain.StuckPR) domain.Message {
	slackPRs := make([]slack.StuckPR, len(prs))
	for i, pr := range prs {
		slackPRs[i] = slack.StuckPR{
			Repository: pr.Repository,
			Number:     pr.Number,
			URL:        pr.URL,
			IdleDays:   pr.IdleDays,
		}
	}
	return toDomainMessage(c.composer.StuckDigestList(slackPRs))
}

// toDomainMessage maps a platform Slack message to the domain's neutral Message.
// The digest emits only section blocks (Text set), so Elements and Buttons on
// the source block carry nothing and are dropped here.
func toDomainMessage(m slack.Message) domain.Message {
	blocks := make([]domain.Block, len(m.Blocks))
	for i, block := range m.Blocks {
		var text *domain.TextObject
		if block.Text != nil {
			text = &domain.TextObject{Type: block.Text.Type, Text: block.Text.Text}
		}
		blocks[i] = domain.Block{Type: block.Type, Text: text}
	}
	return domain.Message{Blocks: blocks, Fallback: m.Fallback}
}

var _ domain.DigestComposer = (*SlackComposer)(nil)
