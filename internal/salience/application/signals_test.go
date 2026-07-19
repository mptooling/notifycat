package application_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/salience/application"
	"github.com/mptooling/notifycat/internal/salience/domain"
)

func TestComputeSignals(t *testing.T) {
	cases := []struct {
		name  string
		title string
		body  string
		files []string
		want  domain.Signals
	}{
		{name: "plain feature", title: "feat: add limiter", want: domain.Signals{}},
		{name: "breaking bang title", title: "feat(api)!: drop v1 endpoints", want: domain.Signals{Breaking: true}},
		{name: "breaking footer in body", title: "feat: split config", body: "detail\n\nBREAKING-CHANGE: config.yaml is now required", want: domain.Signals{Breaking: true}},
		{name: "revert title", title: "Revert \"feat: add limiter\"", want: domain.Signals{Revert: true}},
		{name: "docs only", title: "docs: fix typos", files: []string{"docs/setup.md", "README.md"}, want: domain.Signals{DocsOnly: true}},
		{name: "deps only", title: "chore: bump deps", files: []string{"go.mod", "go.sum"}, want: domain.Signals{DepsOnly: true}},
		{name: "generated only", title: "chore: regen", files: []string{"api/v1/service.pb.go", "internal/mocks/store_gen.go"}, want: domain.Signals{GeneratedOnly: true}},
		{name: "mixed files clear path classes", title: "feat: x", files: []string{"docs/setup.md", "main.go"}, want: domain.Signals{}},
		{name: "no files no path classes", title: "docs: y", files: nil, want: domain.Signals{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := application.ComputeSignals(tc.title, tc.body, tc.files)
			if got != tc.want {
				t.Errorf("ComputeSignals() = %+v; want %+v", got, tc.want)
			}
		})
	}
}
