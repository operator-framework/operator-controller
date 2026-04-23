package serverutil

import (
	"context"
	"crypto/tls"
	"errors"
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
	"sigs.k8s.io/controller-runtime/pkg/healthz"

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

// AddCatalogServerToManager adds the catalog HTTP server to the manager and registers
// a readiness check that passes only when this pod is the leader and actively serving.
// The listener is created lazily inside Start() so non-leader pods never bind the port,
// which ensures the readiness check correctly excludes them from Service endpoints.
func AddCatalogServerToManager(mgr ctrl.Manager, cfg CatalogServerConfig, cw *certwatcher.CertWatcher) error {
	shutdownTimeout := 30 * time.Second
	r := &catalogServerRunnable{
		cfg: cfg,
		cw:  cw,
		server: &http.Server{
			Addr:         cfg.CatalogAddr,
			Handler:      storageServerHandlerWrapped(mgr.GetLogger().WithName("catalogd-http-server"), cfg),
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Minute,
		},
		shutdownTimeout: shutdownTimeout,
		ready:           make(chan struct{}),
	}

	if err := mgr.Add(r); err != nil {
		return fmt.Errorf("error adding catalog server to manager: %w", err)
	}

	// Register a readiness check that passes only once Start() has been called (i.e.
	// this pod holds the leader lease and the catalog server is actively serving).
	// Non-leader pods never reach Start(), so they remain not-ready and are excluded
	// from Service endpoints — preventing catalog traffic from hitting a pod that
	// isn't serving the catalog port.
	if err := mgr.AddReadyzCheck("catalog-server", r.readyzCheck()); err != nil {
		return fmt.Errorf("error adding catalog server readiness check: %w", err)
	}

	return nil
}

// catalogServerRunnable is a leader-only Runnable that binds the catalog HTTP port
// lazily inside Start(), so non-leader pods never hold the listen socket.
type catalogServerRunnable struct {
	cfg             CatalogServerConfig
	cw              *certwatcher.CertWatcher
	server          *http.Server
	shutdownTimeout time.Duration
	// ready is closed by Start() once the server is about to begin serving.
	ready chan struct{}
}

// NeedLeaderElection returns false so the catalog server starts on every pod
// immediately, regardless of leadership.  This is required for rolling updates:
// if Start() were gated on leadership, a new pod could not win the leader lease
// (held by the still-running old pod) and therefore could never pass the
// catalog-server readiness check, deadlocking the rollout.
//
// Non-leader pods serve the catalog HTTP port but have an empty local cache
// (only the leader's reconciler downloads catalog content), so requests to a
// non-leader return 404.  Callers are expected to retry.
func (r *catalogServerRunnable) NeedLeaderElection() bool { return false }

func (r *catalogServerRunnable) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", r.cfg.CatalogAddr)
	if err != nil {
		return fmt.Errorf("error creating catalog server listener: %w", err)
	}

	if r.cfg.CertFile != "" && r.cfg.KeyFile != "" {
		config := &tls.Config{
			GetCertificate: r.cw.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		}
		listener = tls.NewListener(listener, config)
	}

	// Signal readiness before blocking on Serve so the readiness probe passes promptly.
	close(r.ready)

	go func() {
		<-ctx.Done()
		shutdownCtx := context.Background()
		if r.shutdownTimeout > 0 {
			var cancel context.CancelFunc
			shutdownCtx, cancel = context.WithTimeout(shutdownCtx, r.shutdownTimeout)
			defer cancel()
		}
		if err := r.server.Shutdown(shutdownCtx); err != nil {
			// Shutdown errors are logged by the manager; nothing actionable here.
			_ = err
		}
	}()

	if err := r.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// readyzCheck returns a healthz.Checker that passes once Start() has been called.
func (r *catalogServerRunnable) readyzCheck() healthz.Checker {
	return func(_ *http.Request) error {
		select {
		case <-r.ready:
			return nil
		default:
			return fmt.Errorf("catalog server not yet started")
		}
	}
}

func logrLoggingHandler(l logr.Logger, handler http.Handler) http.Handler {
	return handlers.CustomLoggingHandler(nil, handler, func(_ io.Writer, params handlers.LogFormatterParams) {
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
