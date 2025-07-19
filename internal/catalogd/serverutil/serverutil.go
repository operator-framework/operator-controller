package serverutil

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type ServerConfig struct {
	Name                string
	OnlyServeWhenLeader bool
	ListenAddr          string
	GetCertificate      func(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
	Server              *http.Server
	ShutdownTimeout     *time.Duration
}

func NewManagerServer(cfg ServerConfig) (*manager.Server, error) {
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("error creating catalog server listener: %w", err)
	}

	if cfg.GetCertificate != nil {
		listener = tls.NewListener(listener, &tls.Config{
			GetCertificate: cfg.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		})
	}

	catalogServer := manager.Server{
		Name:                cfg.Name,
		OnlyServeWhenLeader: cfg.OnlyServeWhenLeader,
		Server:              cfg.Server,
		ShutdownTimeout:     cfg.ShutdownTimeout,
		Listener:            listener,
	}

	return &catalogServer, nil
}
