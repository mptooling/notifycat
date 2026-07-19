package infrastructure_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	diagnosticsinfra "github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
)

type stubGateway struct {
	response saliencedomain.ModelResponse
	err      error
}

func (s *stubGateway) Generate(context.Context, saliencedomain.ModelRequest) (saliencedomain.ModelResponse, error) {
	return s.response, s.err
}

func TestAIProbeSuccess(t *testing.T) {
	gateway := &stubGateway{response: saliencedomain.ModelResponse{
		Text:      `{"ok":true}`,
		RateLimit: &saliencedomain.RateLimitInfo{RequestsRemaining: 99, RequestsLimit: 100, TokensRemaining: -1, TokensLimit: -1},
	}}
	clock := time.Unix(1750000000, 0)
	probe := diagnosticsinfra.NewAIProbe(gateway, func() time.Time { defer func() { clock = clock.Add(200 * time.Millisecond) }(); return clock })

	result := probe.Probe(context.Background())

	if !result.OK {
		t.Fatalf("probe failed: %+v", result)
	}
	if result.RateLimit != "requests 99/100 remaining" {
		t.Errorf("RateLimit = %q", result.RateLimit)
	}
}

func TestAIProbeNoHeadersReportsNotExposed(t *testing.T) {
	probe := diagnosticsinfra.NewAIProbe(&stubGateway{response: saliencedomain.ModelResponse{Text: `{"ok":true}`}}, time.Now)
	result := probe.Probe(context.Background())
	if !result.OK || result.RateLimit != "no limits exposed by the endpoint (Gemini quota is provider-enforced; see the provider console)" {
		t.Errorf("result = %+v", result)
	}
}

func TestAIProbeRateLimited(t *testing.T) {
	probe := diagnosticsinfra.NewAIProbe(&stubGateway{err: &saliencedomain.RateLimitedError{Detail: "Quota exceeded for metric X", RetryAfter: "30"}}, time.Now)
	result := probe.Probe(context.Background())
	if result.OK {
		t.Fatal("a 429 probe must not be OK")
	}
	if !strings.Contains(result.Detail, "Quota exceeded") || !strings.Contains(result.Detail, "30") {
		t.Errorf("Detail = %q; must surface the provider's quota detail and retry-after", result.Detail)
	}
}

func TestAIProbeTransportError(t *testing.T) {
	probe := diagnosticsinfra.NewAIProbe(&stubGateway{err: errors.New("connection refused")}, time.Now)
	if result := probe.Probe(context.Background()); result.OK {
		t.Fatal("a transport error must not be OK")
	}
}
