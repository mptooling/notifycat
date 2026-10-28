package runtime_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_AgentAccessFlow(t *testing.T) {
	public, adminServer := buildAgentAPIApp(t, agentAPIConfig(t))
	require.NotNil(t, adminServer)
	admin := httptest.NewServer(adminServer.Handler)
	t.Cleanup(admin.Close)
	adminAuth := map[string]string{"Authorization": "Bearer " + testAdminKey}

	createStatus, createBody := request(t, admin, http.MethodPost, "/admin/v1/invites", `{"user":"alice"}`, adminAuth)
	require.Equal(t, http.StatusCreated, createStatus)
	var invite struct {
		Invite        string `json:"invite"`
		RedeemCommand string `json:"redeem_command"`
	}
	require.NoError(t, json.Unmarshal([]byte(createBody), &invite))
	require.Contains(t, invite.RedeemCommand, "https://notifycat.example.com/api/v1/invites/redeem")

	inviteAuth := map[string]string{"Authorization": "Invite " + invite.Invite}
	redeemStatus, redeemBody := request(t, public, http.MethodPost, "/api/v1/invites/redeem", "", inviteAuth)
	require.Equal(t, http.StatusCreated, redeemStatus)
	var redeemed struct {
		User  string `json:"user"`
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal([]byte(redeemBody), &redeemed))
	require.Equal(t, "alice", redeemed.User)
	require.Regexp(t, `^nct_[0-9a-f]{32}_[A-Za-z0-9_-]{43}$`, redeemed.Token)

	redeemAgainStatus, _ := request(t, public, http.MethodPost, "/api/v1/invites/redeem", "", inviteAuth)
	require.Equal(t, http.StatusUnauthorized, redeemAgainStatus, "an invite is single-use")

	tokenAuth := map[string]string{"Authorization": "Bearer " + redeemed.Token}
	probeStatus, probeBody := request(t, public, http.MethodGet, "/api/v1/probe", "", tokenAuth)
	require.Equal(t, http.StatusOK, probeStatus)
	require.JSONEq(t, `{"user":"alice"}`, probeBody)

	listStatus, listBody := request(t, admin, http.MethodGet, "/admin/v1/tokens", "", adminAuth)
	require.Equal(t, http.StatusOK, listStatus)
	var listed struct {
		Tokens []struct {
			ID         string  `json:"id"`
			User       string  `json:"user"`
			LastUsedAt *string `json:"last_used_at"`
		} `json:"tokens"`
	}
	require.NoError(t, json.Unmarshal([]byte(listBody), &listed))
	require.Len(t, listed.Tokens, 1)
	require.Equal(t, "alice", listed.Tokens[0].User)
	require.NotNil(t, listed.Tokens[0].LastUsedAt, "the probe call marked the token used")

	deleteStatus, _ := request(t, admin, http.MethodDelete, "/admin/v1/tokens/"+listed.Tokens[0].ID, "", adminAuth)
	require.Equal(t, http.StatusNoContent, deleteStatus)

	revokedStatus, _ := request(t, public, http.MethodGet, "/api/v1/probe", "", tokenAuth)
	assert.Equal(t, http.StatusUnauthorized, revokedStatus, "deleting the row ends access at once")
}
