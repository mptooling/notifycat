package httpx

import (
	"encoding/json"
	"net/http"
)

type errorBody struct {
	Error string `json:"error"`
}

// WriteJSON writes body as a non-cacheable JSON response.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set(ContentTypeHeader, JSONMediaType)
	w.Header().Set(CacheControlHeader, NoStore)
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(body)
}

// WriteError writes {"error": code} with the status.
func WriteError(w http.ResponseWriter, status int, code string) {
	WriteJSON(w, status, errorBody{Error: code})
}

// WriteNoContent writes a non-cacheable 204.
func WriteNoContent(w http.ResponseWriter) {
	w.Header().Set(CacheControlHeader, NoStore)
	w.WriteHeader(http.StatusNoContent)
}
