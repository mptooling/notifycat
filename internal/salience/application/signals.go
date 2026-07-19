package application

import (
	"path"
	"regexp"
	"strings"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

var (
	// Conventional-commits breaking marker: "type!:" or "type(scope)!:".
	breakingTitlePattern = regexp.MustCompile(`^[A-Za-z]+(\([^)]*\))?!:`)
	// Breaking footer, hyphen or space form, at a line start.
	breakingFooterPattern = regexp.MustCompile(`(?mi)^breaking[- ]change:`)
	revertTitlePattern    = regexp.MustCompile(`(?i)^revert(:|\s|")`)
)

// dependencyManifests are file basenames whose exclusive presence marks a
// dependency-only change.
var dependencyManifests = map[string]bool{
	"go.mod": true, "go.sum": true,
	"package.json": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"requirements.txt": true, "poetry.lock": true, "pipfile.lock": true,
	"gemfile.lock": true, "cargo.toml": true, "cargo.lock": true,
	"composer.json": true, "composer.lock": true,
}

// ComputeSignals derives the rule-sufficient facts about a PR — breaking
// marker, revert pattern, docs-only / deps-only / generated-only path
// classes. Anything a regex answers is computed here and fed to the model as
// a signal, never asked of it. Path classes stay false with no file list.
func ComputeSignals(title, body string, changedFiles []string) domain.Signals {
	signals := domain.Signals{
		Breaking: breakingTitlePattern.MatchString(title) || breakingFooterPattern.MatchString(body),
		Revert:   revertTitlePattern.MatchString(title),
	}
	if len(changedFiles) == 0 {
		return signals
	}
	signals.DocsOnly = allFiles(changedFiles, isDocsPath)
	signals.DepsOnly = allFiles(changedFiles, isDependencyPath)
	signals.GeneratedOnly = allFiles(changedFiles, isGeneratedPath)
	return signals
}

func allFiles(files []string, matches func(string) bool) bool {
	for _, file := range files {
		if !matches(strings.ToLower(file)) {
			return false
		}
	}
	return true
}

func isDocsPath(file string) bool {
	if strings.HasPrefix(file, "docs/") || strings.Contains(file, "/docs/") {
		return true
	}
	switch path.Ext(file) {
	case ".md", ".mdx", ".rst":
		return true
	}
	return false
}

func isDependencyPath(file string) bool {
	return dependencyManifests[path.Base(file)]
}

func isGeneratedPath(file string) bool {
	if strings.HasPrefix(file, "vendor/") || strings.Contains(file, "/vendor/") {
		return true
	}
	if strings.HasPrefix(file, "node_modules/") || strings.Contains(file, "/node_modules/") {
		return true
	}
	return strings.HasSuffix(file, ".pb.go") || strings.HasSuffix(file, "_gen.go") || strings.HasSuffix(file, ".gen.go")
}
