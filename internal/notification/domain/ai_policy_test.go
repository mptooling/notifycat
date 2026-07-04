package domain_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/notification/domain"
)

func TestIsBot_IsBotIdentifiesExactly(t *testing.T) {
	if !domain.IsBot("Bot") {
		t.Error("IsBot(\"Bot\") = false; want true")
	}
	for _, senderType := range []string{"", "bot", "BOT", "Robot", "User"} {
		if domain.IsBot(senderType) {
			t.Errorf("IsBot(%q) = true; want false (only \"Bot\" matches)", senderType)
		}
	}
}
