package store_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/mptooling/notifycat/internal/store"
)

func TestSlackMessages_SaveThenGet(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewSlackMessages(db)
	ctx := context.Background()

	m := store.SlackMessage{PRNumber: 42, Repository: "octo/widget", TS: "1700000000.0001"}
	if err := repo.Save(ctx, m); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.Get(ctx, "octo/widget", 42)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != m {
		t.Fatalf("Get = %+v; want %+v", got, m)
	}
}

func TestSlackMessages_SaveIsIdempotentOnCompositeKey(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewSlackMessages(db)
	ctx := context.Background()

	first := store.SlackMessage{PRNumber: 7, Repository: "octo/widget", TS: "ts-1"}
	if err := repo.Save(ctx, first); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	second := store.SlackMessage{PRNumber: 7, Repository: "octo/widget", TS: "ts-2"}
	if err := repo.Save(ctx, second); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	got, err := repo.Get(ctx, "octo/widget", 7)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TS != "ts-2" {
		t.Fatalf("Get.TS = %q; want %q", got.TS, "ts-2")
	}
}

func TestSlackMessages_GetMissingReturnsErrNotFound(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewSlackMessages(db)
	ctx := context.Background()

	_, err := repo.Get(ctx, "octo/none", 99)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get missing: err = %v; want ErrNotFound", err)
	}
}

func TestSlackMessages_Delete(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewSlackMessages(db)
	ctx := context.Background()

	m := store.SlackMessage{PRNumber: 1, Repository: "o/r", TS: "t"}
	if err := repo.Save(ctx, m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := repo.Delete(ctx, "o/r", 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, "o/r", 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after Delete: err = %v; want ErrNotFound", err)
	}
}

func TestSlackMessages_DeleteMissingIsNoop(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewSlackMessages(db)
	ctx := context.Background()

	if err := repo.Delete(ctx, "o/r", 1); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestRepoMappings_UpsertThenGet(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewRepoMappings(db)
	ctx := context.Background()

	in := store.RepoMapping{
		Repository:   "octo/widget",
		SlackChannel: "C123",
		Mentions:     []string{"@alice", "@bob"},
	}
	out, err := repo.Upsert(ctx, in)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if out.ID == 0 {
		t.Fatalf("Upsert returned ID 0; want auto-assigned")
	}

	got, err := repo.Get(ctx, "octo/widget")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Repository != in.Repository || got.SlackChannel != in.SlackChannel {
		t.Fatalf("Get = %+v; want repo+channel = %+v", got, in)
	}
	if !equalStrings(got.Mentions, in.Mentions) {
		t.Fatalf("Get.Mentions = %v; want %v", got.Mentions, in.Mentions)
	}
}

func TestRepoMappings_UpsertReplacesByRepository(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewRepoMappings(db)
	ctx := context.Background()

	first, err := repo.Upsert(ctx, store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C1", Mentions: []string{"@a"},
	})
	if err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	second, err := repo.Upsert(ctx, store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C2", Mentions: []string{"@b"},
	})
	if err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("Upsert created a second row (IDs %d -> %d); want replace", first.ID, second.ID)
	}

	got, err := repo.Get(ctx, "octo/widget")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SlackChannel != "C2" || !equalStrings(got.Mentions, []string{"@b"}) {
		t.Fatalf("Get = %+v; want channel C2 and mentions [@b]", got)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d; want 1", len(list))
	}
}

func TestRepoMappings_GetMissingReturnsErrNotFound(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewRepoMappings(db)
	ctx := context.Background()

	_, err := repo.Get(ctx, "o/none")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get missing: err = %v; want ErrNotFound", err)
	}
}

func TestRepoMappings_List(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewRepoMappings(db)
	ctx := context.Background()

	for _, m := range []store.RepoMapping{
		{Repository: "o/a", SlackChannel: "C1", Mentions: []string{"@a"}},
		{Repository: "o/b", SlackChannel: "C2", Mentions: []string{}},
	} {
		if _, err := repo.Upsert(ctx, m); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List len = %d; want 2", len(list))
	}
	repos := []string{list[0].Repository, list[1].Repository}
	sort.Strings(repos)
	if repos[0] != "o/a" || repos[1] != "o/b" {
		t.Fatalf("List repos = %v; want [o/a o/b]", repos)
	}
}

func TestRepoMappings_Delete(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewRepoMappings(db)
	ctx := context.Background()

	if _, err := repo.Upsert(ctx, store.RepoMapping{
		Repository: "o/r", SlackChannel: "C1", Mentions: []string{},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := repo.Delete(ctx, "o/r"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, "o/r"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after Delete: err = %v; want ErrNotFound", err)
	}
}

func TestRepoMappings_DeleteMissingReturnsErrNotFound(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewRepoMappings(db)
	ctx := context.Background()

	if err := repo.Delete(ctx, "o/none"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete missing: err = %v; want ErrNotFound", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
