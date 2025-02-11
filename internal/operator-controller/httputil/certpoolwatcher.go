package httputil

import (
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
	generation int
	dir        string
	mx         sync.RWMutex
	pool       *x509.CertPool
	log        logr.Logger
	watcher    *fsnotify.Watcher
	done       chan bool
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

func (cpw *CertPoolWatcher) Done() {
	cpw.done <- true
}

func NewCertPoolWatcher(caDir string, log logr.Logger) (*CertPoolWatcher, error) {
	pool, err := NewCertPool(caDir, log)
	if err != nil {
		return nil, err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// If the SSL_CERT_DIR or SSL_CERT_FILE environment variables are
	// specified, this means that we have some control over the system root
	// location, thus they may change, thus we should watch those locations.
	sslCertDir := os.Getenv("SSL_CERT_DIR")
	sslCertFile := os.Getenv("SSL_CERT_FILE")
	log.V(defaultLogLevel).Info("SSL environment", "SSL_CERT_DIR", sslCertDir, "SSL_CERT_FILE", sslCertFile)

	watchPaths := strings.Split(sslCertDir, ":")
	watchPaths = append(watchPaths, caDir, sslCertFile)
	watchPaths = slices.DeleteFunc(watchPaths, func(p string) bool {
		if p == "" {
			return true
		}
		if _, err := os.Stat(p); err != nil {
			return true
		}
		return false
	})

	for _, p := range watchPaths {
		if err := watcher.Add(p); err != nil {
			return nil, err
		}
		logPath(p, "watching certificate", log)
	}

	cpw := &CertPoolWatcher{
		generation: 1,
		dir:        caDir,
		pool:       pool,
		log:        log,
		watcher:    watcher,
		done:       make(chan bool),
	}
	go func() {
		for {
			select {
			case <-watcher.Events:
				cpw.drainEvents()
				cpw.update()
			case err := <-watcher.Errors:
				log.Error(err, "error watching certificate dir")
				os.Exit(1)
			case <-cpw.done:
				err := watcher.Close()
				if err != nil {
					log.Error(err, "error closing watcher")
				}
				return
			}
		}
	}()
	return cpw, nil
}

func (cpw *CertPoolWatcher) update() {
	cpw.log.Info("updating certificate pool")
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

// Drain as many events as possible before doing anything
// Otherwise, we will be hit with an event for _every_ entry in the
// directory, and end up doing an update for each one
func (cpw *CertPoolWatcher) drainEvents() {
	for {
		drainTimer := time.NewTimer(time.Millisecond * 50)
		select {
		case <-drainTimer.C:
			return
		case <-cpw.watcher.Events:
		}
		if !drainTimer.Stop() {
			<-drainTimer.C
		}
	}
}
