// Package infrastructure holds the routing adapters: the YAML loader for the
// mappings section of config.yaml, the config.lock read/write/diff/merge store,
// and the GitHub changed-files reader. It is the only routing layer that
// touches the filesystem or an external SDK; per the transition-debt rule it
// may import internal/github (and, transitionally, internal/store) until those
// move under platform/.
package infrastructure
