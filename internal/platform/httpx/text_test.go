package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/platform/httpx"
)

func TestWriteText_WritesOneLine(t *testing.T) {
	recorder := httptest.NewRecorder()

	httpx.WriteText(recorder, http.StatusCreated, "curl <<<'a'")

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, "text/plain; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "curl <<<'a'\n", recorder.Body.String())
}

func TestPrefersText(t *testing.T) {
	testCases := []struct {
		name   string
		accept []string
		want   bool
	}{
		{name: "no header", want: false},
		{name: "any type", accept: []string{"*/*"}, want: false},
		{name: "json", accept: []string{"application/json"}, want: false},
		{name: "text", accept: []string{"text/plain"}, want: true},
		{name: "text upper-case", accept: []string{"Text/Plain"}, want: true},
		{name: "text with charset", accept: []string{"text/plain; charset=utf-8"}, want: true},
		{name: "any text", accept: []string{"text/*"}, want: true},
		{name: "text before any type", accept: []string{"text/plain, */*;q=0.1"}, want: true},
		{name: "json and text tie", accept: []string{"application/json, text/plain, */*"}, want: false},
		{name: "text ranked higher", accept: []string{"application/json;q=0.5, text/plain"}, want: true},
		{name: "json ranked higher", accept: []string{"text/plain;q=0.5, application/json"}, want: false},
		{name: "text refused", accept: []string{"text/plain;q=0"}, want: false},
		{name: "exact type wins over range", accept: []string{"text/plain;q=0, text/*"}, want: false},
		{name: "two header lines", accept: []string{"application/json;q=0.1", "text/plain"}, want: true},
		{name: "malformed", accept: []string{"text/plain;;;q=x"}, want: false},
		{name: "quality above one", accept: []string{"text/plain;q=2"}, want: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
			for _, accept := range testCase.accept {
				request.Header.Add("Accept", accept)
			}

			assert.Equal(t, testCase.want, httpx.PrefersText(request))
		})
	}
}
