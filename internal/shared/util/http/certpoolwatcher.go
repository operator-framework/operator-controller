package http

import (
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
)

type CertPoolWatcher struct {
	generation   int
	dir          string
	sslCertPaths []string
	mx           sync.RWMutex
	pool         *x509.CertPool
	log          logr.Logger
	watcher      *fsnotify.Watcher
	done         chan bool
	restart      func(int)
}

// Returns the current CertPool and the generation number
func (cpw *CertPoolWatcher) Get() (*x509.CertPool, int, error) {
	cpw.mx.RLock()
	defer cpw.mx.RUnlock()
	if cpw.pool == nil {
		return nil, 0, fmt.Errorf("no certificate pool available")
	}
	return cpw.pool.Clone(), cpw.generation, nil
}

// Change the restart behavior
func (cpw *CertPoolWatcher) Restart(f func(int)) {
	cpw.restart = f
}

// Indicate that you're done with the CertPoolWatcher so it can terminate
// the watcher go func
func (cpw *CertPoolWatcher) Done() {
	if cpw.watcher != nil {
		cpw.done <- true
	}
}

func (cpw *CertPoolWatcher) Start(ctx context.Context) error {
	var err error
	cpw.pool, err = NewCertPool(cpw.dir, cpw.log)
	if err != nil {
		return err
	}

	watchPaths := append(cpw.sslCertPaths, cpw.dir)
	watchPaths = slices.DeleteFunc(watchPaths, deleteEmptyStrings)

	// Nothing was configured to be watched, which means this is
	// using the SystemCertPool, so we still need to no error out
	if len(watchPaths) == 0 {
		cpw.log.Info("No paths to watch")
		return nil
	}

	cpw.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	for _, p := range watchPaths {
		if err := cpw.watcher.Add(p); err != nil {
			cpw.watcher.Close()
			cpw.watcher = nil
			return err
		}
		logPath(p, "watching certificate", cpw.log)
	}

	go func() {
		for {
			select {
			case e := <-cpw.watcher.Events:
				cpw.checkForRestart(e.Name)
				cpw.drainEvents()
				cpw.update(e.Name)
			case err := <-cpw.watcher.Errors:
				cpw.log.Error(err, "error watching certificate dir")
				os.Exit(1)
			case <-ctx.Done():
				cpw.Done()
			case <-cpw.done:
				err := cpw.watcher.Close()
				if err != nil {
					cpw.log.Error(err, "error closing watcher")
				}
				return
			}
		}
	}()
	return nil
}

func NewCertPoolWatcher(caDir string, log logr.Logger) (*CertPoolWatcher, error) {
	// If the SSL_CERT_DIR or SSL_CERT_FILE environment variables are
	// specified, this means that we have some control over the system root
	// location, thus they may change, thus we should watch those locations.
	//
	// BECAUSE THE SYSTEM POOL WILL NOT UPDATE, WE HAVE TO RESTART IF THERE
	// CHANGES TO EITHER OF THESE LOCATIONS: SSL_CERT_DIR, SSL_CERT_FILE
	//
	sslCertDir := os.Getenv("SSL_CERT_DIR")
	sslCertFile := os.Getenv("SSL_CERT_FILE")
	log.V(defaultLogLevel).Info("SSL environment", "SSL_CERT_DIR", sslCertDir, "SSL_CERT_FILE", sslCertFile)

	sslCertPaths := append(strings.Split(sslCertDir, ":"), sslCertFile)
	sslCertPaths = slices.DeleteFunc(sslCertPaths, deleteEmptyStrings)

	cpw := &CertPoolWatcher{
		generation:   1,
		dir:          caDir,
		sslCertPaths: sslCertPaths,
		log:          log,
		done:         make(chan bool),
	}
	return cpw, nil
}

func deleteEmptyStrings(p string) bool {
	if p == "" {
		return true
	}
	if _, err := os.Stat(p); err != nil {
		return true
	}
	return false
}

func (cpw *CertPoolWatcher) update(name string) {
	cpw.log.Info("updating certificate pool", "file", name)
	pool, err := NewCertPool(cpw.dir, cpw.log)
	if err != nil {
		cpw.log.Error(err, "error updating certificate pool")
		os.Exit(1)
	}
	cpw.mx.Lock()
	defer cpw.mx.Unlock()
	cpw.pool = pool
	cpw.generation++
}

func (cpw *CertPoolWatcher) checkForRestart(name string) {
	for _, p := range cpw.sslCertPaths {
		if strings.Contains(name, p) {
			cpw.log.Info("restarting due to file change", "file", name)
			if cpw.restart != nil {
				cpw.restart(0)
			}
		}
	}
}

// Drain as many events as possible before doing anything
// Otherwise, we will be hit with an event for _every_ entry in the
// directory, and end up doing an update for each one
func (cpw *CertPoolWatcher) drainEvents() {
	for {
		drainTimer := time.NewTimer(time.Millisecond * 50)
		select {
		case <-drainTimer.C:
			return
		case e := <-cpw.watcher.Events:
			cpw.checkForRestart(e.Name)
		}
		if !drainTimer.Stop() {
			<-drainTimer.C
		}
	}
}
