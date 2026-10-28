package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/platform/httpx"
)

func TestWriteJSON_SetsStatusAndHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()

	httpx.WriteJSON(recorder, http.StatusCreated, map[string]string{"user": "alice"})

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.JSONEq(t, `{"user":"alice"}`, recorder.Body.String())
}

func TestWriteError_WritesErrorCode(t *testing.T) {
	recorder := httptest.NewRecorder()

	httpx.WriteError(recorder, http.StatusUnauthorized, "unauthorized")

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, recorder.Body.String())
}

func TestWriteNoContent(t *testing.T) {
	recorder := httptest.NewRecorder()

	httpx.WriteNoContent(recorder)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.Empty(t, recorder.Body.String())
}

func TestWriteJSON_KeepsHTMLCharacters(t *testing.T) {
	recorder := httptest.NewRecorder()

	httpx.WriteJSON(recorder, http.StatusOK, map[string]string{"command": "curl <<<'a & b'"})

	assert.Contains(t, recorder.Body.String(), `"curl <<<'a & b'"`)
	assert.NotContains(t, recorder.Body.String(), "\\u003c")
}
