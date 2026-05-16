package mappingcli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/store"
)

func TestAdd_Then_List(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewRepoMappings(db)
	ctx := context.Background()

	var out, errOut bytes.Buffer
	code := Add(ctx, repo, store.RepoMapping{
		Repository:   "octo/widget",
		SlackChannel: "C123ABCDE",
		Mentions:     []string{"@alice", "@bob"},
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("add exit = %d; stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = List(ctx, repo, &out, &errOut)
	if code != 0 {
		t.Fatalf("list exit = %d; stderr=%s", code, errOut.String())
	}
	listed := out.String()
	if !strings.Contains(listed, "octo/widget") || !strings.Contains(listed, "C123ABCDE") {
		t.Errorf("list output missing fields: %q", listed)
	}
	if !strings.Contains(listed, "@alice") || !strings.Contains(listed, "@bob") {
		t.Errorf("list output missing mentions: %q", listed)
	}
}

func TestAdd_ReplacesExisting(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewRepoMappings(db)
	ctx := context.Background()
	var out, errOut bytes.Buffer

	_ = Add(ctx, repo, store.RepoMapping{Repository: "octo/widget", SlackChannel: "C111", Mentions: []string{"@a"}}, &out, &errOut)
	out.Reset()
	errOut.Reset()
	code := Add(ctx, repo, store.RepoMapping{Repository: "octo/widget", SlackChannel: "C222", Mentions: []string{"@b"}}, &out, &errOut)
	if code != 0 {
		t.Fatalf("replace add exit = %d; stderr=%s", code, errOut.String())
	}

	out.Reset()
	_ = List(ctx, repo, &out, &errOut)
	if !strings.Contains(out.String(), "C222") || strings.Contains(out.String(), "C111") {
		t.Errorf("list after replace = %q", out.String())
	}
}

func TestRemove(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewRepoMappings(db)
	ctx := context.Background()
	var out, errOut bytes.Buffer

	_ = Add(ctx, repo, store.RepoMapping{Repository: "octo/widget", SlackChannel: "C123ABCDE", Mentions: []string{"@a"}}, &out, &errOut)

	out.Reset()
	errOut.Reset()
	code := Remove(ctx, repo, "octo/widget", &out, &errOut)
	if code != 0 {
		t.Fatalf("remove exit = %d; stderr=%s", code, errOut.String())
	}

	if _, err := repo.Get(ctx, "octo/widget"); err == nil {
		t.Errorf("mapping still present after remove")
	}
}

func TestRemove_Missing(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewRepoMappings(db)
	var out, errOut bytes.Buffer

	code := Remove(context.Background(), repo, "octo/none", &out, &errOut)
	if code == 0 {
		t.Fatalf("remove of missing mapping returned 0; stderr=%s", errOut.String())
	}
}
