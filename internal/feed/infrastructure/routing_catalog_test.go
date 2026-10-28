package infrastructure_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/feed/infrastructure"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

type stubEntryLister struct {
	entries []routingdomain.Entry
}

func (lister stubEntryLister) Entries() []routingdomain.Entry { return lister.entries }

func newCatalog() *infrastructure.RoutingCatalog {
	return infrastructure.NewRoutingCatalog(stubEntryLister{entries: []routingdomain.Entry{
		{Org: "Acme", Repo: "API"},
		{Org: "acme", Wildcard: true},
		{Org: "beta", Repo: "svc"},
	}})
}

func TestRoutingCatalog_IsConfigured(t *testing.T) {
	testCases := []struct {
		name       string
		repository string
		want       bool
	}{
		{"explicit entry", "acme/api", true},
		{"case insensitive", "ACME/Api", true},
		{"wildcard org", "acme/anything", true},
		{"other explicit entry", "beta/svc", true},
		{"unconfigured repo in explicit org", "beta/other", false},
		{"unconfigured org", "other/x", false},
		{"no slash", "acme", false},
		{"empty repo", "acme/", false},
		{"star", "*", false},
	}
	catalog := newCatalog()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, catalog.IsConfigured(testCase.repository))
		})
	}
}

func TestRoutingCatalog_Configured(t *testing.T) {
	catalog := newCatalog()

	got := catalog.Configured([]string{"BETA/svc", "other/x", "acme/api", "beta/svc"})

	assert.Equal(t, []string{"beta/svc", "acme/api"}, got)
}
