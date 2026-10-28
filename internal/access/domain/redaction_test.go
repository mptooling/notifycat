package domain_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/access/domain"
)

func TestSecretBearers_NeverPrintTheSecret(t *testing.T) {
	secret := fixtureSecret
	testCases := []struct {
		name  string
		value any
	}{
		{"credential", domain.Credential{Prefix: domain.TokenPrefix, ID: fixtureID, Secret: secret}},
		{"issued invite", domain.IssuedInvite{
			Invite:        domain.Invite{ID: fixtureID},
			Secret:        domain.InvitePrefix + fixtureID + "_" + secret,
			RedeemCommand: domain.RedeemCommand("", domain.InvitePrefix+fixtureID+"_"+secret),
		}},
		{"issued token", domain.IssuedToken{
			Token:  domain.Token{ID: fixtureID},
			Secret: domain.TokenPrefix + fixtureID + "_" + secret,
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var jsonBuffer, textBuffer bytes.Buffer
			slog.New(slog.NewJSONHandler(&jsonBuffer, nil)).Info("x", "value", testCase.value)
			slog.New(slog.NewTextHandler(&textBuffer, nil)).Info("x", "value", testCase.value)
			outputs := []string{
				fmt.Sprintf("%v", testCase.value),
				fmt.Sprintf("%+v", testCase.value),
				fmt.Sprintf("%#v", testCase.value),
				fmt.Sprintf("%s", testCase.value),
				fmt.Sprintf("%d", testCase.value),
				jsonBuffer.String(),
				textBuffer.String(),
			}

			for _, output := range outputs {
				assert.NotContains(t, output, secret)
				assert.Contains(t, output, fixtureID)
			}
		})
	}
}
