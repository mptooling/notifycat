package infrastructure

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/mptooling/notifycat/internal/routing/application"
	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

// FileNotFoundError is returned by Load when the mappings file cannot be opened.
type FileNotFoundError struct {
	Path string
	Err  error
}

func (e *FileNotFoundError) Error() string {
	return fmt.Sprintf("mappings: open %s: %s", e.Path, e.Err)
}

func (e *FileNotFoundError) Unwrap() error { return e.Err }

// ParseError is returned by Load when the mappings file cannot be parsed.
type ParseError struct {
	Path string
	Err  error
}

func (e *ParseError) Error() string { return e.Err.Error() }

func (e *ParseError) Unwrap() error { return e.Err }

// Parse reads + validates the YAML document. Unknown keys and shape errors
// are returned as errors (the server fails fast at startup).
//
// `mentions:` is optional: an absent key means "ping @channel"; `mentions: []`
// means "ping nobody"; `mentions: null` is rejected (ambiguous).
func Parse(r io.Reader) (domain.File, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var wire struct {
		Digest   *digestConfigWire                    `yaml:"digest"`
		Mappings map[string]map[string]repoConfigWire `yaml:"mappings"`
	}
	if err := dec.Decode(&wire); err != nil {
		return domain.File{}, fmt.Errorf("mappings: parse: %w", err)
	}
	out := domain.File{Mappings: mappingsToDomain(wire.Mappings)}
	if wire.Digest != nil {
		d := wire.Digest.toDomain()
		out.Digest = &d
	}
	if err := application.ValidateMappings(out.Mappings); err != nil {
		return domain.File{}, err
	}
	return out, nil
}

// DecodeMappings decodes the `mappings:` section — captured by the caller as a
// raw YAML node — into the tri-state routing model. Decoding through the wire
// types preserves the mentions absent/[]/value distinction that a plain struct
// decode collapses (absent and [] would otherwise both read as "no key"). An
// absent (zero) node yields no mappings.
func DecodeMappings(node *yaml.Node) (map[string]domain.Org, error) {
	if node == nil || node.Kind == 0 {
		return nil, nil
	}
	var wire map[string]map[string]repoConfigWire
	if err := node.Decode(&wire); err != nil {
		return nil, fmt.Errorf("mappings: parse: %w", err)
	}
	return mappingsToDomain(wire), nil
}

// Load reads and validates the file at path.
func Load(path string) (*application.Provider, error) {
	f, err := os.Open(path) //nolint:gosec // path is operator-supplied configuration
	if err != nil {
		return nil, &FileNotFoundError{Path: path, Err: err}
	}
	defer func() { _ = f.Close() }()

	file, err := Parse(f)
	if err != nil {
		return nil, &ParseError{Path: path, Err: err}
	}
	return application.NewProvider(domain.Defaults{}, file.Mappings, file.Digest), nil
}
