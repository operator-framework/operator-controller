package profile

import (
	"context"
	"net/http"
	"net/http/pprof"
)

type profileConfig struct {
	pprof   bool
	cmdline bool
	profile bool
	symbol  bool
	trace   bool
}

// Option applies a configuration option to the given config.
type Option func(p *profileConfig)

func (p *profileConfig) apply(options []Option) {
	if len(options) == 0 {
		// If no options are given, default to all
		p.pprof = true
		p.cmdline = true
		p.profile = true
		p.symbol = true
		p.trace = true

		return
	}

	for _, o := range options {
		o(p)
	}
}

func defaultProfileConfig() *profileConfig {
	// Initialize config
	return &profileConfig{}
}

// RegisterHandlers registers profile Handlers with the given ServeMux.
//
// The Handlers registered are determined by the given options.
// If no options are given, all available handlers are registered by default.
func RegisterHandlers(mux *http.ServeMux, options ...Option) {
	config := defaultProfileConfig()
	config.apply(options)

	if config.pprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
	}
	if config.cmdline {
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	}
	if config.profile {
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	}
	if config.symbol {
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	}
	if config.trace {
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}
}

/* TODO(everettraven): Update this to:
1. Have a struct/object implementation that implements the controller-runtime Runnable interface
*/

type Pprofer struct {
	pprof   bool
	cmdline bool
	profile bool
	symbol  bool
	trace   bool
	port    int
	mux     *http.ServeMux
}

func NewPprofer(opts ...PproferOptions) *Pprofer {
	// Create a new Pprofer && default all options
	// to true. If someone doesn't want the default
	// value of true they can change it with an option.
	// Default port is 8080
	pprofer := &Pprofer{
		pprof:   true,
		cmdline: true,
		profile: true,
		symbol:  true,
		trace:   true,
		port:    8080,
	}

	// Apply any options that have been specified
	for _, opt := range opts {
		opt(pprofer)
	}

	// Create the new ServeMux and add the necessary paths and handlers
	mux := http.NewServeMux()

	if pprofer.pprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
	}
	if pprofer.cmdline {
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	}
	if pprofer.profile {
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	}
	if pprofer.symbol {
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	}
	if pprofer.trace {
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	pprofer.mux = mux

	return pprofer
}

// implements the Runnable interface
func (p *Pprofer) Start(ctx context.Context) error {
	return http.ListenAndServe("localhost:8080", p.mux)
}

// Options
type PproferOptions func(*Pprofer)

func WithPort(port int) PproferOptions {
	return func(p *Pprofer) {
		p.port = port
	}
}

func WithIndex(val bool) PproferOptions {
	return func(p *Pprofer) {
		p.pprof = val
	}
}

func WithCmdline(val bool) PproferOptions {
	return func(p *Pprofer) {
		p.cmdline = val
	}
}

func WithProfile(val bool) PproferOptions {
	return func(p *Pprofer) {
		p.profile = val
	}
}

func WithSymbol(val bool) PproferOptions {
	return func(p *Pprofer) {
		p.symbol = val
	}
}

func WithTrace(val bool) PproferOptions {
	return func(p *Pprofer) {
		p.trace = val
	}
}
