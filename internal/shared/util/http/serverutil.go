package http

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
	TLSConfig           *tls.Config
	Server              *http.Server
	ShutdownTimeout     *time.Duration
}

func NewManagerServer(cfg ServerConfig) (*manager.Server, error) {
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("error creating catalog server listener: %w", err)
	}

	if cfg.TLSConfig != nil {
		listener = tls.NewListener(listener, cfg.TLSConfig)
	}

	srv := manager.Server{
		Name:                cfg.Name,
		OnlyServeWhenLeader: cfg.OnlyServeWhenLeader,
		Server:              cfg.Server,
		ShutdownTimeout:     cfg.ShutdownTimeout,
		Listener:            listener,
	}

	return &srv, nil
}
