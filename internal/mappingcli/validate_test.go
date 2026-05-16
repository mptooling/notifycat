package mappingcli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/store"
	"github.com/mptooling/notifycat/internal/validate"
)

// fakeValidator is the test double the CLI sees through the Validator
// interface. The factory wiring matches main()'s seam, so cmdValidate is
// exercised without touching real Slack/GitHub clients.
type fakeValidator struct {
	validate    func(ctx context.Context, repository string) validate.Report
	validateAll func(ctx context.Context) ([]validate.Report, error)
}

func (f *fakeValidator) Validate(ctx context.Context, repository string) validate.Report {
	return f.validate(ctx, repository)
}

func (f *fakeValidator) ValidateAll(ctx context.Context) ([]validate.Report, error) {
	return f.validateAll(ctx)
}

func factoryFor(v *fakeValidator) ValidatorFactory {
	return func(_ *store.RepoMappings) (Validator, error) {
		return v, nil
	}
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

func TestRun_Validate_AllPass(t *testing.T) {
	fv := &fakeValidator{
		validate: func(_ context.Context, repository string) validate.Report {
			return okReport(repository)
		},
	}
	var out, errOut bytes.Buffer
	code := run([]string{"validate", "acme/widgets"}, store.NewTestDB(t), &out, &errOut, factoryFor(fv))
	if code != 0 {
		t.Fatalf("validate exit = %d; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "acme/widgets") || !strings.Contains(out.String(), "OK") {
		t.Errorf("validate output = %q", out.String())
	}
}

func TestRun_Validate_FailsExits1(t *testing.T) {
	fv := &fakeValidator{
		validate: func(_ context.Context, repository string) validate.Report {
			return validate.Report{
				Repository: repository,
				Checks: []validate.CheckResult{
					{Name: "slack-channel", Status: validate.StatusFail, Detail: "bot is not a member of #x"},
				},
			}
		},
	}
	var out, errOut bytes.Buffer
	code := run([]string{"validate", "acme/widgets"}, store.NewTestDB(t), &out, &errOut, factoryFor(fv))
	if code != 1 {
		t.Fatalf("validate exit = %d; want 1; stdout=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "FAIL") || !strings.Contains(out.String(), "not a member") {
		t.Errorf("validate output should surface failure, got %q", out.String())
	}
}

func TestRun_Validate_NoArg_IteratesAllMappings(t *testing.T) {
	fv := &fakeValidator{
		validateAll: func(_ context.Context) ([]validate.Report, error) {
			return []validate.Report{okReport("a/b"), okReport("c/d")}, nil
		},
	}
	var out, errOut bytes.Buffer
	code := run([]string{"validate"}, store.NewTestDB(t), &out, &errOut, factoryFor(fv))
	if code != 0 {
		t.Fatalf("validate exit = %d; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "a/b") || !strings.Contains(out.String(), "c/d") {
		t.Errorf("expected both repos in output, got %q", out.String())
	}
}

func TestRun_Validate_NoArg_EmptyMappings(t *testing.T) {
	fv := &fakeValidator{
		validateAll: func(_ context.Context) ([]validate.Report, error) { return nil, nil },
	}
	var out, errOut bytes.Buffer
	code := run([]string{"validate"}, store.NewTestDB(t), &out, &errOut, factoryFor(fv))
	if code != 0 {
		t.Fatalf("validate empty exit = %d", code)
	}
	if !strings.Contains(out.String(), "no mappings to validate") {
		t.Errorf("expected hint about empty mappings, got %q", out.String())
	}
}

func TestRun_Validate_FactoryError(t *testing.T) {
	factory := func(_ *store.RepoMappings) (Validator, error) {
		return nil, errors.New("boom")
	}
	var out, errOut bytes.Buffer
	code := run([]string{"validate"}, store.NewTestDB(t), &out, &errOut, factory)
	if code != 1 {
		t.Fatalf("validate factory-error exit = %d; want 1", code)
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Errorf("stderr should surface factory error, got %q", errOut.String())
	}
}

func TestRun_Validate_TooManyArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"validate", "a/b", "c/d"}, store.NewTestDB(t), &out, &errOut, factoryFor(&fakeValidator{}))
	if code != 2 {
		t.Fatalf("validate too-many-args exit = %d; want 2", code)
	}
}
