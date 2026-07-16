package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// countingGateway is a thread-safe fake that can fail N times then succeed.
type countingGateway struct {
	mu       sync.Mutex
	calls    int
	err      error
	response domain.ModelResponse
}

func (g *countingGateway) Generate(_ context.Context, _ domain.ModelRequest) (domain.ModelResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.err != nil {
		return domain.ModelResponse{}, g.err
	}
	return g.response, nil
}

func (g *countingGateway) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func validOpenText() string {
	return `{"targets":[{"channel":"C0000000001","loudness":"ping","mentions":["<@U1>"],"leading_emoji":"eyes","format":"standard","emphasis":"none","context_block":"","thread_note":""}],"rationale":"fine"}`
}

func resilientParams(gateway domain.ModelGateway, now func() time.Time) domain.AdvisorParams {
	return domain.AdvisorParams{
		Config:  domain.Config{Enabled: true, Provider: domain.ProviderGemini, Model: "gemini-2.5-flash", Instructions: "global"},
		Gateway: gateway,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:     now,
	}
}

func TestResilientAdvisorTierDisabledSkipsModel(t *testing.T) {
	gateway := &countingGateway{response: domain.ModelResponse{Text: validOpenText()}}
	advisor := NewResilientAdvisor(resilientParams(gateway, time.Now))
	request := modelOpenRequest()
	request.TierEnabled = false

	decision := advisor.DecideOpen(context.Background(), request)

	if decision.FallbackReason != domain.FallbackDisabled {
		t.Errorf("FallbackReason = %q; want disabled", decision.FallbackReason)
	}
	if gateway.callCount() != 0 {
		t.Error("gateway must not be consulted for an opted-out tier")
	}
}

func TestResilientAdvisorCachesDecisions(t *testing.T) {
	gateway := &countingGateway{response: domain.ModelResponse{Text: validOpenText()}}
	advisor := NewResilientAdvisor(resilientParams(gateway, time.Now))
	request := modelOpenRequest()

	first := advisor.DecideOpen(context.Background(), request)
	second := advisor.DecideOpen(context.Background(), request)

	if gateway.callCount() != 1 {
		t.Fatalf("gateway calls = %d; a duplicate delivery must hit the cache", gateway.callCount())
	}
	if first.CacheHit || !second.CacheHit {
		t.Errorf("CacheHit flags wrong: first=%v second=%v", first.CacheHit, second.CacheHit)
	}
	if second.Targets[0].Channel != "C0000000001" {
		t.Errorf("cached decision content lost: %+v", second.Targets)
	}
}

func TestResilientAdvisorCircuitOpensAfterConsecutiveFailures(t *testing.T) {
	gateway := &countingGateway{err: errors.New("connection refused")}
	clock := time.Unix(1750000000, 0)
	advisor := NewResilientAdvisor(resilientParams(gateway, func() time.Time { return clock }))

	for i := 0; i < domain.CircuitFailureThreshold; i++ {
		request := modelOpenRequest()
		request.PR.Number = 100 + i // distinct cache keys
		decision := advisor.DecideOpen(context.Background(), request)
		if decision.FallbackReason != domain.FallbackTransportError {
			t.Fatalf("call %d FallbackReason = %q; want transport_error", i, decision.FallbackReason)
		}
	}
	request := modelOpenRequest()
	request.PR.Number = 999
	decision := advisor.DecideOpen(context.Background(), request)
	if decision.FallbackReason != domain.FallbackCircuitOpen {
		t.Errorf("FallbackReason = %q; want circuit_open after %d failures", decision.FallbackReason, domain.CircuitFailureThreshold)
	}
	if gateway.callCount() != domain.CircuitFailureThreshold {
		t.Errorf("gateway calls = %d; the open circuit must skip the gateway", gateway.callCount())
	}
}

func TestResilientAdvisorCircuitHalfOpensAfterCooldown(t *testing.T) {
	gateway := &countingGateway{err: errors.New("connection refused")}
	clock := time.Unix(1750000000, 0)
	now := func() time.Time { return clock }
	advisor := NewResilientAdvisor(resilientParams(gateway, now))

	for i := 0; i < domain.CircuitFailureThreshold; i++ {
		request := modelOpenRequest()
		request.PR.Number = 100 + i
		advisor.DecideOpen(context.Background(), request)
	}
	gateway.mu.Lock()
	gateway.err = nil
	gateway.response = domain.ModelResponse{Text: validOpenText()}
	gateway.mu.Unlock()

	clock = clock.Add(domain.CircuitOpenDuration + time.Second)
	request := modelOpenRequest()
	request.PR.Number = 999
	decision := advisor.DecideOpen(context.Background(), request)
	if decision.FallbackReason != domain.FallbackNone {
		t.Errorf("FallbackReason = %q; the half-open probe must reach the recovered gateway", decision.FallbackReason)
	}
}

func TestResilientAdvisorFillsSignalsAndGlobalDigestInstructions(t *testing.T) {
	recorder := &recordingGateway{response: domain.ModelResponse{Text: validOpenText()}}
	advisor := NewResilientAdvisor(resilientParams(recorder, time.Now))
	request := modelOpenRequest()
	request.PR.Title = "feat(api)!: drop v1"

	advisor.DecideOpen(context.Background(), request)

	if len(recorder.requests) != 1 || !strings.Contains(recorder.requests[0].User, "breaking=true") {
		t.Error("signals must be computed and fed to the prompt")
	}

	recorder.response = domain.ModelResponse{Text: `{"order":[0],"highlights":["normal"],"notes":[""],"parent_loudness":"ping","rationale":"r"}`}
	advisor.DecideDigest(context.Background(), domain.DigestDecisionRequest{Channel: "C1", PRs: []domain.DigestPRSummary{{Repository: "acme/api", Number: 1, IdleDays: 2}}})
	if !strings.Contains(recorder.requests[1].System, "global") {
		t.Error("digest requests must carry the global instructions")
	}
}

// recordingGateway records requests and returns a canned response.
type recordingGateway struct {
	mu       sync.Mutex
	requests []domain.ModelRequest
	response domain.ModelResponse
}

func (g *recordingGateway) Generate(_ context.Context, request domain.ModelRequest) (domain.ModelResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, request)
	return g.response, nil
}

func TestNewAdvisorBindings(t *testing.T) {
	deterministic := NewAdvisor(domain.AdvisorParams{Config: domain.Config{Enabled: false}})
	if _, ok := deterministic.(*DeterministicAdvisor); !ok {
		t.Errorf("disabled config must bind the deterministic advisor; got %T", deterministic)
	}
	resilient := NewAdvisor(resilientParams(&countingGateway{}, time.Now))
	if _, ok := resilient.(*ResilientAdvisor); !ok {
		t.Errorf("enabled config must bind the resilient advisor; got %T", resilient)
	}
	nilGateway := NewAdvisor(domain.AdvisorParams{Config: domain.Config{Enabled: true}})
	if _, ok := nilGateway.(*DeterministicAdvisor); !ok {
		t.Errorf("enabled without a gateway must bind deterministic; got %T", nilGateway)
	}
}
