package mappingcli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/store"
)

func TestRun_Add_Then_List(t *testing.T) {
	db := store.NewTestDB(t)

	var out, errOut bytes.Buffer
	code := run([]string{"add", "octo/widget", "C123ABCDE", "@alice,@bob"}, db, &out, &errOut, nil)
	if code != 0 {
		t.Fatalf("add exit = %d; stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"list"}, db, &out, &errOut, nil)
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

func TestRun_Add_ReplacesExisting(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	_ = run([]string{"add", "octo/widget", "C111", "@a"}, db, &out, &errOut, nil)
	out.Reset()
	errOut.Reset()
	code := run([]string{"add", "octo/widget", "C222", "@b"}, db, &out, &errOut, nil)
	if code != 0 {
		t.Fatalf("replace add exit = %d; stderr=%s", code, errOut.String())
	}

	out.Reset()
	_ = run([]string{"list"}, db, &out, &errOut, nil)
	if !strings.Contains(out.String(), "C222") || strings.Contains(out.String(), "C111") {
		t.Errorf("list after replace = %q", out.String())
	}
}

func TestRun_Add_InvalidRepositoryRejected(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := run([]string{"add", "invalid-repo-format", "C123", "@a"}, db, &out, &errOut, nil)
	if code == 0 {
		t.Fatalf("add with invalid repo accepted; stderr=%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "repository") {
		t.Errorf("stderr should mention 'repository': %q", errOut.String())
	}
}

func TestRun_Add_InvalidChannelRejected(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := run([]string{"add", "octo/widget", "not-a-channel", "@a"}, db, &out, &errOut, nil)
	if code == 0 {
		t.Fatalf("add with invalid channel accepted; stderr=%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "channel") {
		t.Errorf("stderr should mention 'channel': %q", errOut.String())
	}
}

func TestRun_Remove(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	_ = run([]string{"add", "octo/widget", "C123ABCDE", "@a"}, db, &out, &errOut, nil)

	out.Reset()
	errOut.Reset()
	code := run([]string{"remove", "octo/widget"}, db, &out, &errOut, nil)
	if code != 0 {
		t.Fatalf("remove exit = %d; stderr=%s", code, errOut.String())
	}

	repo := store.NewRepoMappings(db)
	if _, err := repo.Get(context.Background(), "octo/widget"); err == nil {
		t.Errorf("mapping still present after remove")
	}
}

func TestRun_Remove_Missing(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := run([]string{"remove", "octo/none"}, db, &out, &errOut, nil)
	if code == 0 {
		t.Fatalf("remove of missing mapping returned 0; stderr=%s", errOut.String())
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := run([]string{"unknown"}, db, &out, &errOut, nil)
	if code == 0 {
		t.Fatal("unknown subcommand returned 0")
	}
	if !strings.Contains(errOut.String(), "usage") && !strings.Contains(errOut.String(), "unknown") {
		t.Errorf("stderr should hint at usage: %q", errOut.String())
	}
}

func TestRun_EmptyMentionsAllowed(t *testing.T) {
	db := store.NewTestDB(t)
	var out, errOut bytes.Buffer

	code := run([]string{"add", "octo/widget", "C123ABCDE", ""}, db, &out, &errOut, nil)
	if code != 0 {
		t.Fatalf("add with empty mentions exit = %d; stderr=%s", code, errOut.String())
	}
}
