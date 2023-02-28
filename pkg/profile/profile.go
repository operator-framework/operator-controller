package profile

import (
	"net/http"
	"net/http/pprof"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// An update method of adding pprof to the controller manager.
type Pprofer struct {
	pprof   bool
	cmdline bool
	profile bool
	symbol  bool
	trace   bool
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
	}

	// Apply any options that have been specified
	for _, opt := range opts {
		opt(pprofer)
	}

	// Create the new ServeMux and add the necessary paths and handlers
	mux := http.NewServeMux()

	pprofer.mux = mux

	return pprofer
}

type PprofHandler struct {
	Handle http.HandlerFunc
}

var _ http.Handler = &PprofHandler{}

func (h *PprofHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	h.Handle(rw, req)
}

func (p *Pprofer) ConfigureControllerManager(mgr manager.Manager) error {
	if p.pprof {
		if err := mgr.AddMetricsExtraHandler("/debug/pprof/", &PprofHandler{Handle: pprof.Index}); err != nil {
			return err
		}
	}
	if p.cmdline {
		if err := mgr.AddMetricsExtraHandler("/debug/pprof/cmdline", &PprofHandler{Handle: pprof.Cmdline}); err != nil {
			return err
		}
	}
	if p.profile {
		if err := mgr.AddMetricsExtraHandler("/debug/pprof/profile", &PprofHandler{Handle: pprof.Profile}); err != nil {
			return err
		}
	}
	if p.symbol {
		if err := mgr.AddMetricsExtraHandler("/debug/pprof/symbol", &PprofHandler{Handle: pprof.Symbol}); err != nil {
			return err
		}
	}
	if p.trace {
		if err := mgr.AddMetricsExtraHandler("/debug/pprof/trace", &PprofHandler{Handle: pprof.Trace}); err != nil {
			return err
		}
	}

	return nil
}

// Options
type PproferOptions func(*Pprofer)

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
