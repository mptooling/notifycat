package infrastructure

import (
	"net/http"
	"strings"

	"github.com/mptooling/notifycat/internal/access/domain"
)

func credentialFromHeader(request *http.Request, scheme string) string {
	headerScheme, credential, found := strings.Cut(request.Header.Get(domain.AuthorizationHeader), " ")
	if !found || !strings.EqualFold(headerScheme, scheme) {
		return ""
	}
	return strings.TrimSpace(credential)
}
