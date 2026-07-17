package application_test

import (
	"context"
	"strings"
	"testing"

	diagnosticsapp "github.com/mptooling/notifycat/internal/diagnostics/application"
	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

type fakeAIProber struct {
	result diagnosticsdomain.AIProbeResult
	called bool
}

func (f *fakeAIProber) Probe(context.Context) diagnosticsdomain.AIProbeResult {
	f.called = true
	return f.result
}

func checkNamed(t *testing.T, section diagnosticsdomain.Section, name string) validationdomain.CheckResult {
	t.Helper()
	for _, check := range section.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("section %q has no check %q: %+v", section.Name, name, section.Checks)
	return validationdomain.CheckResult{}
}

func aiSection(t *testing.T, sections []diagnosticsdomain.Section) diagnosticsdomain.Section {
	t.Helper()
	for _, section := range sections {
		if section.Name == "ai" {
			return section
		}
	}
	t.Fatal("no ai section in the report")
	return diagnosticsdomain.Section{}
}

func TestCheckAIDisabled(t *testing.T) {
	section := diagnosticsapp.CheckAI(diagnosticsdomain.ConfigSnapshot{AIEnabled: false})
	check := checkNamed(t, section, "ai.enabled")
	if check.Status != validationdomain.StatusOK || !strings.Contains(check.Detail, "disabled") {
		t.Errorf("disabled check = %+v", check)
	}
}

func TestCheckAIEnabledShape(t *testing.T) {
	section := diagnosticsapp.CheckAI(diagnosticsdomain.ConfigSnapshot{
		AIEnabled: true, AIProvider: "gemini", AIModel: "gemini-2.5-flash", AIKeySet: true,
	})
	if checkNamed(t, section, "ai.provider").Status != validationdomain.StatusOK {
		t.Error("known provider must be OK")
	}
	if checkNamed(t, section, "ai.model").Status != validationdomain.StatusOK {
		t.Error("set model must be OK")
	}
	key := checkNamed(t, section, "AI_API_KEY")
	if key.Status != validationdomain.StatusOK || key.Detail != "set" {
		t.Errorf("key check must report presence only, never the value: %+v", key)
	}
}

func TestCheckAIGeminiMissingKeyFails(t *testing.T) {
	section := diagnosticsapp.CheckAI(diagnosticsdomain.ConfigSnapshot{
		AIEnabled: true, AIProvider: "gemini", AIModel: "m", AIKeySet: false,
	})
	if checkNamed(t, section, "AI_API_KEY").Status != validationdomain.StatusFail {
		t.Error("gemini without a key must FAIL")
	}
}

func TestDoctorRunsProbeWhenEnabled(t *testing.T) {
	prober := &fakeAIProber{result: diagnosticsdomain.AIProbeResult{OK: true, Detail: "responded", LatencyMS: 412, RateLimit: "requests 99/100 remaining"}}
	doctor := diagnosticsapp.NewDoctor(diagnosticsdomain.ConfigSnapshot{AIEnabled: true, AIProvider: "openai_compatible", AIModel: "m", AIBaseURL: "http://x"}, nil, prober)

	sections := doctor.Run(context.Background(), "")

	section := aiSection(t, sections)
	if !prober.called {
		t.Fatal("prober not invoked")
	}
	probe := checkNamed(t, section, "probe")
	if probe.Status != validationdomain.StatusOK || !strings.Contains(probe.Detail, "412") {
		t.Errorf("probe check = %+v", probe)
	}
	if checkNamed(t, section, "rate limits").Detail != "requests 99/100 remaining" {
		t.Errorf("rate limit check = %+v", checkNamed(t, section, "rate limits"))
	}
}

func TestDoctorSkipsProbeWhenDisabledOrNil(t *testing.T) {
	prober := &fakeAIProber{}
	doctor := diagnosticsapp.NewDoctor(diagnosticsdomain.ConfigSnapshot{AIEnabled: false}, nil, prober)
	sections := doctor.Run(context.Background(), "")
	if prober.called {
		t.Error("prober must not run with ai disabled")
	}
	aiSection(t, sections) // the section itself still reports "disabled"

	nilProberDoctor := diagnosticsapp.NewDoctor(diagnosticsdomain.ConfigSnapshot{AIEnabled: true, AIProvider: "gemini", AIModel: "m", AIKeySet: true}, nil, nil)
	nilProberDoctor.Run(context.Background(), "") // must not panic
}
