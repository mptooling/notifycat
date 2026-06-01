package mappings

import "fmt"

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
