package application

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mptooling/notifycat/internal/validation/domain"
)

func skip(name, detail string) domain.CheckResult {
	return domain.CheckResult{Name: name, Status: domain.StatusSkip, Detail: detail}
}

// failResult wraps a Sprintf-formatted fail message in a CheckResult so
// individual check methods read as one expression per branch.
func failResult(name, format string, args ...any) domain.CheckResult {
	return domain.CheckResult{
		Name:   name,
		Status: domain.StatusFail,
		Detail: fmt.Sprintf(format, args...),
	}
}

// missingScopes returns required entries absent from have, preserving the
// required-list order so error messages are deterministic.
func missingScopes(have, required []string) []string {
	present := make(map[string]struct{}, len(have))
	for _, s := range have {
		present[s] = struct{}{}
	}
	var missing []string
	for _, r := range required {
		if _, ok := present[r]; !ok {
			missing = append(missing, r)
		}
	}
	return missing
}

// quoteJoin renders items as a sorted, comma-separated, quoted list — used
// inside operator-facing error details.
func quoteJoin(items []string) string {
	cp := append([]string(nil), items...)
	sort.Strings(cp)
	quoted := make([]string, len(cp))
	for i, s := range cp {
		quoted[i] = strconv.Quote(s)
	}
	return strings.Join(quoted, ", ")
}

func splitRepository(repository string) (owner, repo string, ok bool) {
	i := strings.IndexByte(repository, '/')
	if i <= 0 || i == len(repository)-1 {
		return "", "", false
	}
	return repository[:i], repository[i+1:], true
}
