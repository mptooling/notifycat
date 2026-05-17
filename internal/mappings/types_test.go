package mappings

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRepositories_UnmarshalYAML_Wildcard(t *testing.T) {
	var r Repositories
	if err := yaml.Unmarshal([]byte(`"*"`), &r); err != nil {
		t.Fatalf("unmarshal wildcard: %v", err)
	}
	if !r.All || len(r.List) != 0 {
		t.Errorf("wildcard parse: got %+v; want All=true List=nil", r)
	}
}

func TestRepositories_UnmarshalYAML_List(t *testing.T) {
	var r Repositories
	if err := yaml.Unmarshal([]byte(`["api", "web"]`), &r); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if r.All {
		t.Errorf("list shape set All=true")
	}
	if len(r.List) != 2 || r.List[0] != "api" || r.List[1] != "web" {
		t.Errorf("list parse: got %+v", r.List)
	}
}

func TestRepositories_UnmarshalYAML_RejectsStarInList(t *testing.T) {
	var r Repositories
	err := yaml.Unmarshal([]byte(`["api", "*"]`), &r)
	if err == nil || !strings.Contains(err.Error(), `"*"`) {
		t.Fatalf(`expected "*" rejection in list shape; got %v`, err)
	}
}

func TestRepositories_UnmarshalYAML_RejectsEmptyList(t *testing.T) {
	var r Repositories
	err := yaml.Unmarshal([]byte(`[]`), &r)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty-list rejection; got %v", err)
	}
}

func TestRepositories_UnmarshalYAML_RejectsRandomString(t *testing.T) {
	var r Repositories
	err := yaml.Unmarshal([]byte(`"all"`), &r)
	if err == nil {
		t.Fatalf(`expected rejection of non-"*" string`)
	}
}
