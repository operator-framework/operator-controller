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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	catalogdmetrics "github.com/operator-framework/operator-controller/internal/catalogd/metrics"
	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

const catalogdLeaderLabel = "olm.operatorframework.io/catalogd-leader"

type CatalogServerConfig struct {
	ExternalAddr string
	CatalogAddr  string
	CertFile     string
	KeyFile      string
	LocalStorage storage.Instance
	PodName      string
	PodNamespace string
	// TLSOpts are optional functions applied to the TLS configuration when serving over HTTPS.
	// Use these to configure cipher suites, minimum TLS version, curve preferences, and
	// certificate retrieval (e.g. via a certwatcher).
	TLSOpts []func(*tls.Config)
}

// AddCatalogServerToManager adds the catalog HTTP server to the manager and registers
// a readiness check that passes once the server has started serving. Because
// NeedLeaderElection returns false, every replica binds the catalog port and becomes
// ready. catalogd-service selects only the leader-labelled replica.
func AddCatalogServerToManager(mgr ctrl.Manager, cfg CatalogServerConfig) error {
	if cfg.PodName == "" || cfg.PodNamespace == "" {
		return errors.New("catalog server requires the catalogd pod name and namespace")
	}

	shutdownTimeout := 30 * time.Second
	r := &catalogServerRunnable{
		cfg: cfg,
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

	// The Service selects this label, so only the elected pod is an endpoint.
	// This runnable itself is not leader-gated: it first removes a potentially
	// stale label on every pod, then waits for this manager to become leader.
	leaderLabeler := &catalogdLeaderLabeler{
		client:    mgr.GetClient(),
		storage:   cfg.LocalStorage,
		electedCh: mgr.Elected(),
		pod: types.NamespacedName{
			Name:      cfg.PodName,
			Namespace: cfg.PodNamespace,
		},
	}
	if err := mgr.Add(leaderLabeler); err != nil {
		return fmt.Errorf("error adding catalog leader labeler to manager: %w", err)
	}

	// Register a readiness check that passes once Start() has been called and the
	// server is actively serving. All pods reach Start() (NeedLeaderElection=false)
	// so rolling updates do not wait for a replacement pod to win leadership.
	if err := mgr.AddReadyzCheck("catalog-server", r.readyzCheck()); err != nil {
		return fmt.Errorf("error adding catalog server readiness check: %w", err)
	}

	return nil
}

// catalogServerRunnable is a Runnable that binds the catalog HTTP port on every pod.
// Because NeedLeaderElection returns false, Start() is called on all replicas immediately.
type catalogServerRunnable struct {
	cfg             CatalogServerConfig
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
// Non-leader pods serve the catalog HTTP port but are excluded from catalogd-service
// by the leader label because only the leader's reconciler downloads catalog content.
func (r *catalogServerRunnable) NeedLeaderElection() bool { return false }

func (r *catalogServerRunnable) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", r.cfg.CatalogAddr)
	if err != nil {
		return fmt.Errorf("error creating catalog server listener: %w", err)
	}

	if r.cfg.CertFile != "" && r.cfg.KeyFile != "" {
		// All TLS settings (GetCertificate, MinVersion, CipherSuites, NextProtos, etc.)
		// are applied exclusively via TLSOpts, keeping TLS policy out of serverutil.
		config := &tls.Config{} //nolint:gosec
		for _, opt := range r.cfg.TLSOpts {
			opt(config)
		}
		if config.GetCertificate == nil && config.GetConfigForClient == nil && len(config.Certificates) == 0 {
			return fmt.Errorf("catalog server TLS misconfiguration: TLSOpts must configure a certificate source")
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
			// Shutdown errors (e.g. context deadline exceeded) are not actionable;
			// the process is terminating regardless.
			_ = err
		}
	}()

	if err := r.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("catalog server on %q failed: %w", r.cfg.CatalogAddr, err)
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

// catalogdLeaderLabeler manages the label selected by catalogd-service. It starts
// on every replica so a pod that restarts after losing leadership cannot retain a
// stale leader label. It only adds the label after it has become leader and has a
// local copy of every catalog that is currently advertised as serving.
type catalogdLeaderLabeler struct {
	client    client.Client
	storage   storage.Instance
	electedCh <-chan struct{}
	pod       types.NamespacedName
}

// NeedLeaderElection returns false so the label is removed from every replica
// before it can receive traffic. Start waits for the manager's Elected channel
// before adding it back to the elected replica.
func (r *catalogdLeaderLabeler) NeedLeaderElection() bool { return false }

func (r *catalogdLeaderLabeler) Start(ctx context.Context) error {
	if err := r.setLeaderLabel(ctx, false); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("removing catalogd leader label from pod %s: %w", r.pod, err)
	}

	select {
	case <-ctx.Done():
		return nil
	case <-r.electedCh:
	}

	if err := r.waitForCatalogContent(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	if err := r.setLeaderLabel(ctx, true); err != nil {
		return fmt.Errorf("adding catalogd leader label to pod %s: %w", r.pod, err)
	}

	<-ctx.Done()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.setLeaderLabel(cleanupCtx, false); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("removing catalogd leader label from pod %s during shutdown: %w", r.pod, err)
	}
	return nil
}

func (r *catalogdLeaderLabeler) waitForCatalogContent(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		ready, err := r.catalogContentAvailable(ctx)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *catalogdLeaderLabeler) catalogContentAvailable(ctx context.Context) (bool, error) {
	var catalogs ocv1.ClusterCatalogList
	if err := r.client.List(ctx, &catalogs); err != nil {
		return false, fmt.Errorf("listing ClusterCatalogs while waiting to serve content: %w", err)
	}
	for _, catalog := range catalogs.Items {
		if meta.IsStatusConditionPresentAndEqual(catalog.Status.Conditions, ocv1.TypeServing, metav1.ConditionTrue) && !r.storage.ContentExists(catalog.Name) {
			return false, nil
		}
	}
	return true, nil
}

func (r *catalogdLeaderLabeler) setLeaderLabel(ctx context.Context, leader bool) error {
	value := "null"
	if leader {
		value = `"true"`
	}
	patch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":%s}}}`, catalogdLeaderLabel, value))
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: r.pod.Name, Namespace: r.pod.Namespace}}
	return r.client.Patch(ctx, pod, client.RawPatch(types.MergePatchType, patch))
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
