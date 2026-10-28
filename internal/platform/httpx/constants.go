package httpx

// Response caching headers set on every credential-bearing response.
const (
	CacheControlHeader = "Cache-Control"
	NoStore            = "no-store"
)

// AllowHeader lists the methods a 405 response accepts.
const AllowHeader = "Allow"
