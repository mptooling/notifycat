package mappingcli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/validate"
)

// stubChecker is the in-package test double for the Checker interface, so
// the mappingsValidator struct can be exercised without real clients.
type stubChecker struct {
	validate    func(ctx context.Context, repository string) validate.Report
	validateAll func(ctx context.Context) ([]validate.Report, error)
}

func (s *stubChecker) Validate(ctx context.Context, repository string) validate.Report {
	return s.validate(ctx, repository)
}

func (s *stubChecker) ValidateAll(ctx context.Context) ([]validate.Report, error) {
	return s.validateAll(ctx)
}

func okReport(repository string) validate.Report {
	return validate.Report{
		Repository: repository,
		Checks: []validate.CheckResult{
			{Name: "mapping", Status: validate.StatusOK, Detail: "found"},
			{Name: "slack-auth", Status: validate.StatusOK, Detail: "ok"},
		},
	}
}

func TestMappingsValidator_AllPass(t *testing.T) {
	v := newMappingsValidator(&stubChecker{
		validate: func(_ context.Context, repository string) validate.Report {
			return okReport(repository)
		},
	})
	var out, errOut bytes.Buffer
	code := v.Validate(context.Background(), "acme/widgets", &out, &errOut)
	if code != 0 {
		t.Fatalf("validate exit = %d; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "acme/widgets") || !strings.Contains(out.String(), "OK") {
		t.Errorf("validate output = %q", out.String())
	}
}

func TestMappingsValidator_FailsExits1(t *testing.T) {
	v := newMappingsValidator(&stubChecker{
		validate: func(_ context.Context, repository string) validate.Report {
			return validate.Report{
				Repository: repository,
				Checks: []validate.CheckResult{
					{Name: "slack-channel", Status: validate.StatusFail, Detail: "bot is not a member of #x"},
				},
			}
		},
	})
	var out, errOut bytes.Buffer
	code := v.Validate(context.Background(), "acme/widgets", &out, &errOut)
	if code != 1 {
		t.Fatalf("validate exit = %d; want 1; stdout=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "FAIL") || !strings.Contains(out.String(), "not a member") {
		t.Errorf("validate output should surface failure, got %q", out.String())
	}
}

func TestMappingsValidator_NoTarget_IteratesAllMappings(t *testing.T) {
	v := newMappingsValidator(&stubChecker{
		validateAll: func(_ context.Context) ([]validate.Report, error) {
			return []validate.Report{okReport("a/b"), okReport("c/d")}, nil
		},
	})
	var out, errOut bytes.Buffer
	code := v.Validate(context.Background(), "", &out, &errOut)
	if code != 0 {
		t.Fatalf("validate exit = %d; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "a/b") || !strings.Contains(out.String(), "c/d") {
		t.Errorf("expected both repos in output, got %q", out.String())
	}
}

func TestMappingsValidator_NoTarget_EmptyMappings(t *testing.T) {
	v := newMappingsValidator(&stubChecker{
		validateAll: func(_ context.Context) ([]validate.Report, error) { return nil, nil },
	})
	var out, errOut bytes.Buffer
	code := v.Validate(context.Background(), "", &out, &errOut)
	if code != 0 {
		t.Fatalf("validate empty exit = %d", code)
	}
	if !strings.Contains(out.String(), "no mappings to validate") {
		t.Errorf("expected hint about empty mappings, got %q", out.String())
	}
}

func TestMappingsValidator_NoTarget_CheckerError(t *testing.T) {
	v := newMappingsValidator(&stubChecker{
		validateAll: func(_ context.Context) ([]validate.Report, error) {
			return nil, errors.New("boom")
		},
	})
	var out, errOut bytes.Buffer
	code := v.Validate(context.Background(), "", &out, &errOut)
	if code != 1 {
		t.Fatalf("validate checker-error exit = %d; want 1", code)
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Errorf("stderr should surface checker error, got %q", errOut.String())
	}
}
