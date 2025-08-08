package middleware

import (
	"net/http"
)

type Middleware func(http.Handler) http.Handler

func Standard(handler http.Handler) http.Handler {
	return Chain(
		LoggingHandler,
		MetricsHandler,
		GzipHandler,
	)(handler)
}

func Chain(middlewares ...Middleware) Middleware {
	return func(handler http.Handler) http.Handler {
		for _, middleware := range middlewares {
			handler = middleware(handler)
		}
		return handler
	}
}
