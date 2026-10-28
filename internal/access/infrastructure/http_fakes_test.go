package infrastructure_test

import (
	"context"

	"github.com/mptooling/notifycat/internal/access/domain"
)

const validToken = "nct_0123456789abcdef0123456789abcdef_" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"

type fakeTokenAuthenticator struct {
	principal domain.Principal
	err       error
	gotRaw    string
}

func (fake *fakeTokenAuthenticator) Authenticate(_ context.Context, rawToken string) (domain.Principal, error) {
	fake.gotRaw = rawToken
	return fake.principal, fake.err
}

type fakeAdminAuthenticator struct {
	err    error
	calls  int
	gotRaw string
}

func (fake *fakeAdminAuthenticator) Authenticate(_ context.Context, rawKey string) error {
	fake.calls++
	fake.gotRaw = rawKey
	return fake.err
}

type fakeInviteIssuer struct {
	issued  domain.IssuedInvite
	err     error
	gotUser string
}

func (fake *fakeInviteIssuer) Issue(_ context.Context, user string) (domain.IssuedInvite, error) {
	fake.gotUser = user
	return fake.issued, fake.err
}

type fakeTokenAdmin struct {
	tokens    []domain.Token
	err       error
	deleted   []string
	listCalls int
}

func (fake *fakeTokenAdmin) List(_ context.Context) ([]domain.Token, error) {
	fake.listCalls++
	return fake.tokens, fake.err
}

func (fake *fakeTokenAdmin) Delete(_ context.Context, id string) error {
	if fake.err != nil {
		return fake.err
	}
	fake.deleted = append(fake.deleted, id)
	return nil
}

type fakeInviteRedeemer struct {
	issued domain.IssuedToken
	err    error
	gotRaw string
}

func (fake *fakeInviteRedeemer) Redeem(_ context.Context, rawInvite string) (domain.IssuedToken, error) {
	fake.gotRaw = rawInvite
	return fake.issued, fake.err
}
