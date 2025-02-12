package serverutil

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/handlers"
	"github.com/klauspost/compress/gzhttp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	catalogdmetrics "github.com/operator-framework/operator-controller/internal/catalogd/metrics"
	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

type CatalogServerConfig struct {
	ExternalAddr string
	CatalogAddr  string
	CertFile     string
	KeyFile      string
	LocalStorage storage.Instance
}

func AddCatalogServerToManager(mgr ctrl.Manager, cfg CatalogServerConfig, tlsFileWatcher *certwatcher.CertWatcher) error {
	listener, err := net.Listen("tcp", cfg.CatalogAddr)
	if err != nil {
		return fmt.Errorf("error creating catalog server listener: %w", err)
	}

	if cfg.CertFile != "" && cfg.KeyFile != "" {
		// Use the passed certificate watcher instead of creating a new one
		config := &tls.Config{
			GetCertificate: tlsFileWatcher.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		}
		listener = tls.NewListener(listener, config)
	}

	shutdownTimeout := 30 * time.Second
	catalogServer := manager.Server{
		Name:                "catalogs",
		OnlyServeWhenLeader: true,
		Server: &http.Server{
			Addr:        cfg.CatalogAddr,
			Handler:     storageServerHandlerWrapped(mgr.GetLogger().WithName("catalogd-http-server"), cfg),
			ReadTimeout: 5 * time.Second,
			// TODO: Revert this to 10 seconds if/when the API
			// evolves to have significantly smaller responses
			WriteTimeout: 5 * time.Minute,
		},
		ShutdownTimeout: &shutdownTimeout,
		Listener:        listener,
	}

	err = mgr.Add(&catalogServer)
	if err != nil {
		return fmt.Errorf("error adding catalog server to manager: %w", err)
	}

	return nil
}

func logrLoggingHandler(l logr.Logger, handler http.Handler) http.Handler {
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

		l.Info("handled request", "host", host, "username", username, "method", params.Request.Method, "uri", uri, "protocol", params.Request.Proto, "status", params.StatusCode, "size", params.Size)
	})
}

func storageServerHandlerWrapped(l logr.Logger, cfg CatalogServerConfig) http.Handler {
	handler := cfg.LocalStorage.StorageServerHandler()
	handler = gzhttp.GzipHandler(handler)
	handler = catalogdmetrics.AddMetricsToHandler(handler)

	handler = logrLoggingHandler(l, handler)
	return handler
}
