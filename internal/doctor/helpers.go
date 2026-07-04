package doctor

import (
	"fmt"

	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// okResult, failResult, and skip build the three CheckResult variants so each
// check body reads as one expression per branch instead of a four-line struct
// literal. They mirror the helpers in the sibling internal/validate package
// (skip / failResult), keeping the two packages' check code shaped alike.
func okResult(name, detail string) validationdomain.CheckResult {
	return validationdomain.CheckResult{Name: name, Status: validationdomain.StatusOK, Detail: detail}
}

func failResult(name, format string, args ...any) validationdomain.CheckResult {
	return validationdomain.CheckResult{
		Name:   name,
		Status: validationdomain.StatusFail,
		Detail: fmt.Sprintf(format, args...),
	}
}

func skip(name, detail string) validationdomain.CheckResult {
	return validationdomain.CheckResult{Name: name, Status: validationdomain.StatusSkip, Detail: detail}
}
