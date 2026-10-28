package infrastructure

import (
	"net/http"
	"strings"
)

func credentialFromHeader(request *http.Request, scheme string) string {
	headerScheme, credential, found := strings.Cut(request.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(headerScheme, scheme) {
		return ""
	}
	return strings.TrimSpace(credential)
}
