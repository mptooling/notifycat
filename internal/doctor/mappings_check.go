package doctor

import (
	"fmt"

	"github.com/mptooling/notifycat/internal/mappings"
)

// CheckMappingsFile loads the mappings file via internal/mappings.Load and
// reports whether the file exists and parses cleanly. An empty mappings map
// is OK (the server treats it as a no-op).
func CheckMappingsFile(path string) Section {
	sec := Section{Name: "mappings"}
	if path == "" {
		sec.Checks = append(sec.Checks, failResult("file", "NOTIFYCAT_MAPPINGS_FILE is empty; set it or rely on the ./mappings.yaml default"))
		return sec
	}
	provider, err := mappings.Load(path)
	if err != nil {
		sec.Checks = append(sec.Checks, failResult("file", "cannot load %q: %v", path, err))
		return sec
	}
	sec.Checks = append(sec.Checks, okResult("file", path))

	entries := provider.Entries()
	if len(entries) == 0 {
		sec.Checks = append(sec.Checks, okResult("entries", "0 entries (server will boot but route nothing)"))
		return sec
	}
	sec.Checks = append(sec.Checks, okResult("entries", fmt.Sprintf("%d entries", len(entries))))
	return sec
}
