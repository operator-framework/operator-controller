package serverutil

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/log"

	catalogdmetrics "github.com/operator-framework/operator-controller/catalogd/internal/metrics"
	"github.com/operator-framework/operator-controller/catalogd/internal/storage"
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
			Addr:    cfg.CatalogAddr,
			Handler: catalogdmetrics.AddMetricsToHandler(cfg.LocalStorage.StorageServerHandler()),
			BaseContext: func(_ net.Listener) context.Context {
				return log.IntoContext(context.Background(), mgr.GetLogger().WithName("http.catalogs"))
			},
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
