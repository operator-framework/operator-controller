package middleware

import (
	"io"
	"net"
	"net/http"

	"github.com/gorilla/handlers"
	"k8s.io/klog/v2"
)

func LoggingHandler(handler http.Handler) http.Handler {
	return handlers.CustomLoggingHandler(nil, handler, func(_ io.Writer, params handlers.LogFormatterParams) {
		// extract parameters used in apache common log format, but then log using `logr` to remain consistent
		// with other loggers used in this codebase.
		username := "-"
		if params.URL.User != nil {
			if name := params.URL.User.Username(); name != "" {
				username = name
			}
		}

		host, _, err := net.SplitHostPort(params.Request.RemoteAddr)
		if err != nil {
			host = params.Request.RemoteAddr
		}

		uri := params.Request.RequestURI
		if params.Request.ProtoMajor == 2 && params.Request.Method == http.MethodConnect {
			uri = params.Request.Host
		}
		if uri == "" {
			uri = params.URL.RequestURI()
		}

		l := klog.FromContext(params.Request.Context()).WithName("catalogd-http-server")
		l.Info("handled request", "host", host, "username", username, "method", params.Request.Method, "uri", uri, "protocol", params.Request.Proto, "status", params.StatusCode, "size", params.Size)
	})
}
