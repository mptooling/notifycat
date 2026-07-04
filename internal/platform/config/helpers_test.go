package config_test

import "fmt"

// stringWithVerb formats v using the given Sprintf verb. Lives in its own
// helper so it stays out of the table-driven test body.
func stringWithVerb(verb string, v any) string {
	return fmt.Sprintf(verb, v)
}
