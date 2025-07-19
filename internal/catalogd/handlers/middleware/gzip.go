package middleware

import (
	"net/http"

	"github.com/klauspost/compress/gzhttp"
)

func GzipHandler(handler http.Handler) http.Handler {
	return gzhttp.GzipHandler(handler)
}
