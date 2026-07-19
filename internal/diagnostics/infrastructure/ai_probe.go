package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
)

// probeTimeout bounds the doctor's live provider call — generous compared to
// the runtime decision timeout because a human is waiting, not a webhook.
const probeTimeout = 15 * time.Second

// probeSchema asks for the smallest possible structured response.
const probeSchema = `{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`

// AIProbe implements diagnosticsdomain.AIProber over the salience model
// gateway: a one-token structured-output call proving key validity and model
// availability, measuring latency, and summarizing rate-limit headroom.
type AIProbe struct {
	gateway saliencedomain.ModelGateway
	now     func() time.Time
}

// NewAIProbe builds an AIProbe. now supplies the latency clock (time.Now in
// production).
func NewAIProbe(gateway saliencedomain.ModelGateway, now func() time.Time) *AIProbe {
	return &AIProbe{gateway: gateway, now: now}
}

// Probe implements diagnosticsdomain.AIProber.
func (p *AIProbe) Probe(ctx context.Context) diagnosticsdomain.AIProbeResult {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	started := p.now()
	response, err := p.gateway.Generate(probeCtx, saliencedomain.ModelRequest{
		System:          "Respond with JSON only.",
		User:            `Return exactly {"ok": true}.`,
		Schema:          json.RawMessage(probeSchema),
		MaxOutputTokens: 16,
	})
	latency := p.now().Sub(started).Milliseconds()

	var rateLimited *saliencedomain.RateLimitedError
	if errors.As(err, &rateLimited) {
		detail := fmt.Sprintf("provider rate limited: %s", rateLimited.Detail)
		if rateLimited.RetryAfter != "" {
			detail += fmt.Sprintf(" (retry after %s)", rateLimited.RetryAfter)
		}
		return diagnosticsdomain.AIProbeResult{Detail: detail + " — check the provider's quota console", LatencyMS: latency}
	}
	if err != nil {
		return diagnosticsdomain.AIProbeResult{Detail: fmt.Sprintf("provider unreachable: %v", err), LatencyMS: latency}
	}

	var parsed struct {
		OK bool `json:"ok"`
	}
	if unmarshalErr := json.Unmarshal([]byte(response.Text), &parsed); unmarshalErr != nil {
		return diagnosticsdomain.AIProbeResult{Detail: fmt.Sprintf("provider responded but not with the requested JSON shape: %v", unmarshalErr), LatencyMS: latency}
	}
	return diagnosticsdomain.AIProbeResult{
		OK:        true,
		Detail:    "responded",
		LatencyMS: latency,
		RateLimit: rateLimitSummary(response.RateLimit),
	}
}

// rateLimitSummary renders best-effort headroom. Endpoints without the
// headers (Gemini bare keys, most local endpoints) report as not exposed.
func rateLimitSummary(info *saliencedomain.RateLimitInfo) string {
	if info == nil {
		return "no limits exposed by the endpoint (Gemini quota is provider-enforced; see the provider console)"
	}
	summary := fmt.Sprintf("requests %d/%d remaining", info.RequestsRemaining, info.RequestsLimit)
	if info.TokensRemaining >= 0 {
		summary += fmt.Sprintf(", tokens %d/%d remaining", info.TokensRemaining, info.TokensLimit)
	}
	return summary
}

var _ diagnosticsdomain.AIProber = (*AIProbe)(nil)
