package store_test

import (
	"context"
	"errors"
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
