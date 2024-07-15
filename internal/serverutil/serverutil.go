package serverutil

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"

	catalogdmetrics "github.com/operator-framework/catalogd/internal/metrics"
	"github.com/operator-framework/catalogd/internal/storage"
	"github.com/operator-framework/catalogd/internal/third_party/server"
)

type CatalogServerConfig struct {
	ExternalAddr string
	CatalogAddr  string
	CertFile     string
	KeyFile      string
	LocalStorage storage.Instance
}

func AddCatalogServerToManager(mgr ctrl.Manager, cfg CatalogServerConfig) error {
	listener, err := net.Listen("tcp", cfg.CatalogAddr)
	if err != nil {
		return fmt.Errorf("error creating catalog server listener: %w", err)
	}

	if cfg.CertFile != "" && cfg.KeyFile != "" {
		tlsFileWatcher, err := certwatcher.New(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return fmt.Errorf("error creating TLS certificate watcher: %w", err)
		}
		config := &tls.Config{
			GetCertificate: tlsFileWatcher.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		}
		err = mgr.Add(tlsFileWatcher)
		if err != nil {
			return fmt.Errorf("error adding TLS certificate watcher to manager: %w", err)
		}
		listener = tls.NewListener(listener, config)
	}

	shutdownTimeout := 30 * time.Second

	catalogServer := server.Server{
		Kind: "catalogs",
		Server: &http.Server{
			Addr:        cfg.CatalogAddr,
			Handler:     catalogdmetrics.AddMetricsToHandler(cfg.LocalStorage.StorageServerHandler()),
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
