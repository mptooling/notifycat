package doctor

import (
	"fmt"

	"github.com/mptooling/notifycat/internal/mappings"
)

// CheckMappings reports whether the `mappings:` section of config.yaml parsed
// into any entries. An empty section is OK (the server boots but routes
// nothing). Parse failures already surface in config load, so by the time the
// doctor has a provider the file is structurally valid.
func CheckMappings(provider *mappings.Provider) Section {
	sec := Section{Name: "mappings"}
	entries := provider.Entries()
	if len(entries) == 0 {
		sec.Checks = append(sec.Checks, okResult("entries", "0 entries (server will boot but route nothing)"))
		return sec
	}
	sec.Checks = append(sec.Checks, okResult("entries", fmt.Sprintf("%d entries", len(entries))))
	return sec
}
