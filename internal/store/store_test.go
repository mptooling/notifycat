package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

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
	if got.PRNumber != m.PRNumber || got.Repository != m.Repository || got.TS != m.TS {
		t.Fatalf("Get = %+v; want %+v", got, m)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("Get.UpdatedAt is zero; want auto-populated timestamp")
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

func TestSlackMessages_Save_BumpsUpdatedAt(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewSlackMessages(db)
	ctx := context.Background()

	m := store.SlackMessage{PRNumber: 1, Repository: "o/r", TS: "t1"}
	if err := repo.Save(ctx, m); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	first, err := repo.Get(ctx, "o/r", 1)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	// Sleep long enough to clear SQLite's CURRENT_TIMESTAMP one-second resolution.
	time.Sleep(1100 * time.Millisecond)
	if err := repo.Save(ctx, store.SlackMessage{PRNumber: 1, Repository: "o/r", TS: "t2"}); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	second, err := repo.Get(ctx, "o/r", 1)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("second UpdatedAt (%v) not after first (%v)", second.UpdatedAt, first.UpdatedAt)
	}
}

func TestSlackMessages_DeleteStaleBefore_RemovesOldRows(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewSlackMessages(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	old1 := now.Add(-72 * time.Hour)
	old2 := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)

	seed := []store.SlackMessage{
		{PRNumber: 1, Repository: "o/r", TS: "ts-old1", UpdatedAt: old1},
		{PRNumber: 2, Repository: "o/r", TS: "ts-old2", UpdatedAt: old2},
		{PRNumber: 3, Repository: "o/r", TS: "ts-fresh", UpdatedAt: fresh},
	}
	for _, m := range seed {
		// Bypass autoUpdateTime by writing the column explicitly via raw GORM.
		if err := store.RawCreateForTest(db, m); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	cutoff := now.Add(-24 * time.Hour)
	deleted, err := repo.DeleteStaleBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteStaleBefore: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("DeleteStaleBefore returned %d; want 2", deleted)
	}

	for _, pr := range []int{1, 2} {
		if _, err := repo.Get(ctx, "o/r", pr); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("PR %d still present after delete: err = %v", pr, err)
		}
	}
	if _, err := repo.Get(ctx, "o/r", 3); err != nil {
		t.Errorf("fresh row removed unexpectedly: %v", err)
	}
}

func TestSlackMessages_DeleteStaleBefore_Empty(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewSlackMessages(db)
	ctx := context.Background()

	deleted, err := repo.DeleteStaleBefore(ctx, time.Now())
	if err != nil {
		t.Fatalf("DeleteStaleBefore on empty: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("DeleteStaleBefore on empty returned %d; want 0", deleted)
	}
}
