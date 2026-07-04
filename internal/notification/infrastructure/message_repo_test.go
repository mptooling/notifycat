package infrastructure

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func TestMessageRepo_Messages_MapsRows(t *testing.T) {
	db := persistence.NewTestDB(t)
	pullRequests := persistence.NewPullRequests(db)
	repo := NewMessageRepo(pullRequests)

	if err := pullRequests.AddMessage(context.Background(), "acme/api", 42, "C_ACME", "ts1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := repo.Messages(context.Background(), "acme/api", 42)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	want := []domain.Message{{Channel: "C_ACME", MessageID: "ts1"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Messages = %+v; want %+v", got, want)
	}
}

func TestMessageRepo_Messages_UnknownPRReturnsNotFound(t *testing.T) {
	db := persistence.NewTestDB(t)
	repo := NewMessageRepo(persistence.NewPullRequests(db))
	if _, err := repo.Messages(context.Background(), "ghost/x", 1); !errors.Is(err, routingdomain.ErrNotFound) {
		t.Errorf("err = %v; want routingdomain.ErrNotFound", err)
	}
}
