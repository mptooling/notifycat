package runtime

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/mptooling/notifycat/internal/platform/config"
	salienceapp "github.com/mptooling/notifycat/internal/salience/application"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
	"github.com/mptooling/notifycat/internal/salience/infrastructure/gemini"
	"github.com/mptooling/notifycat/internal/salience/infrastructure/openaicompat"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestBuildAdvisorDisabledBindsDeterministic(t *testing.T) {
	cfg := config.Config{}
	advisor := buildAdvisor(&http.Client{}, cfg, testLogger())
	if _, ok := advisor.(*salienceapp.DeterministicAdvisor); !ok {
		t.Errorf("advisor = %T; want deterministic with ai disabled", advisor)
	}
}

func TestBuildAdvisorEnabledBindsResilient(t *testing.T) {
	cfg := config.Config{AI: saliencedomain.Config{Enabled: true, Provider: saliencedomain.ProviderGemini, Model: "gemini-2.5-flash"}, AIAPIKey: config.Secret("k")}
	advisor := buildAdvisor(&http.Client{}, cfg, testLogger())
	if _, ok := advisor.(*salienceapp.ResilientAdvisor); !ok {
		t.Errorf("advisor = %T; want resilient with ai enabled", advisor)
	}
}

func TestSalienceGatewayProviderSelection(t *testing.T) {
	geminiConfig := config.Config{AI: saliencedomain.Config{Enabled: true, Provider: saliencedomain.ProviderGemini, Model: "m"}, AIAPIKey: config.Secret("k")}
	if gateway := salienceGateway(&http.Client{}, geminiConfig); gateway == nil {
		t.Fatal("gemini gateway not built")
	} else if _, ok := gateway.(*gemini.Client); !ok {
		t.Errorf("gateway = %T; want *gemini.Client", gateway)
	}

	compatConfig := config.Config{AI: saliencedomain.Config{Enabled: true, Provider: saliencedomain.ProviderOpenAICompatible, Model: "m", BaseURL: "http://localhost:11434/v1"}}
	if gateway := salienceGateway(&http.Client{}, compatConfig); gateway == nil {
		t.Fatal("openaicompat gateway not built")
	} else if _, ok := gateway.(*openaicompat.Client); !ok {
		t.Errorf("gateway = %T; want *openaicompat.Client", gateway)
	}

	disabled := config.Config{}
	if gateway := salienceGateway(&http.Client{}, disabled); gateway != nil {
		t.Errorf("gateway = %T; want nil with ai disabled — no AI code path active", gateway)
	}
}
