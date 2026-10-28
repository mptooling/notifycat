package httpx

// Response caching headers set on every credential-bearing response.
const (
	CacheControlHeader = "Cache-Control"
	NoStore            = "no-store"
)

// AllowHeader lists the methods a 405 response accepts.
const AllowHeader = "Allow"

// Content negotiation headers and media types.
const (
	AcceptHeader      = "Accept"
	ContentTypeHeader = "Content-Type"
	JSONMediaType     = "application/json"
	TextMediaType     = "text/plain"
	TextContentType   = TextMediaType + "; charset=utf-8"
	QualityParameter  = "q"
)
