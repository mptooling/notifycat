package httpx

import "net/http"

// Route pairs a ServeMux pattern with its handler.
type Route struct {
	Pattern string
	Handler http.Handler
}
