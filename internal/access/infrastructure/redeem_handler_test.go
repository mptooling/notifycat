package infrastructure_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/access/infrastructure"
)

func redeemRequest(header string) *http.Request {
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/invites/redeem", nil)
	if header != "" {
		request.Header.Set("Authorization", header)
	}
	return request
}

func TestRedeemHandler_ReturnsToken(t *testing.T) {
	redeemer := &fakeInviteRedeemer{issued: domain.IssuedToken{Token: domain.Token{User: "alice"}, Secret: "nct_y"}}
	recorder := httptest.NewRecorder()

	infrastructure.NewRedeemHandler(redeemer, discardLogger()).ServeHTTP(recorder, redeemRequest("Invite nci_x"))

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.JSONEq(t, `{"user":"alice","token":"nct_y"}`, recorder.Body.String())
	assert.Equal(t, "nci_x", redeemer.gotRaw)
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}

func TestRedeemHandler_EveryFailureIsTheSame401(t *testing.T) {
	testCases := []struct {
		name   string
		header string
		err    error
	}{
		{name: "no header", err: &domain.AuthError{Reason: domain.AuthFailureMalformed}},
		{name: "wrong scheme", header: "Bearer nci_x", err: &domain.AuthError{Reason: domain.AuthFailureMalformed}},
		{name: "used", header: "Invite nci_x", err: &domain.AuthError{Reason: domain.AuthFailureUsed}},
		{name: "expired", header: "Invite nci_x", err: &domain.AuthError{Reason: domain.AuthFailureExpired}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			redeemer := &fakeInviteRedeemer{err: testCase.err}
			recorder := httptest.NewRecorder()

			infrastructure.NewRedeemHandler(redeemer, discardLogger()).ServeHTTP(recorder, redeemRequest(testCase.header))

			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.JSONEq(t, `{"error":"unauthorized"}`, recorder.Body.String())
			assert.Equal(t, "Invite", recorder.Header().Get("WWW-Authenticate"))
		})
	}
}

func TestRedeemHandler_WrongSchemeSendsEmptyCredential(t *testing.T) {
	redeemer := &fakeInviteRedeemer{err: &domain.AuthError{Reason: domain.AuthFailureMalformed}}

	infrastructure.NewRedeemHandler(redeemer, discardLogger()).ServeHTTP(httptest.NewRecorder(), redeemRequest("Bearer nci_x"))

	assert.Empty(t, redeemer.gotRaw)
}

func TestRedeemHandler_StoreErrorIs500(t *testing.T) {
	redeemer := &fakeInviteRedeemer{err: errors.New("disk")}
	recorder := httptest.NewRecorder()

	infrastructure.NewRedeemHandler(redeemer, discardLogger()).ServeHTTP(recorder, redeemRequest("Invite nci_x"))

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.JSONEq(t, `{"error":"internal_error"}`, recorder.Body.String())
}
