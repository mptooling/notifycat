package doctor

import (
	"fmt"

	"github.com/mptooling/notifycat/internal/validate"
)

// okResult, failResult, and skip build the three CheckResult variants so each
// check body reads as one expression per branch instead of a four-line struct
// literal. They mirror the helpers in the sibling internal/validate package
// (skip / failResult), keeping the two packages' check code shaped alike.
func okResult(name, detail string) validate.CheckResult {
	return validate.CheckResult{Name: name, Status: validate.StatusOK, Detail: detail}
}

func failResult(name, format string, args ...any) validate.CheckResult {
	return validate.CheckResult{
		Name:   name,
		Status: validate.StatusFail,
		Detail: fmt.Sprintf(format, args...),
	}
}

func skip(name, detail string) validate.CheckResult {
	return validate.CheckResult{Name: name, Status: validate.StatusSkip, Detail: detail}
}
