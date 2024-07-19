package httputil

import (
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
)

type CertPoolWatcher struct {
	dir     string
	mx      sync.RWMutex
	pool    *x509.CertPool
	log     logr.Logger
	watcher *fsnotify.Watcher
}

func (cpw *CertPoolWatcher) Get() (*x509.CertPool, error) {
	if cpw.pool == nil {
		return nil, fmt.Errorf("no certificate pool available")
	}
	cpw.mx.RLock()
	defer cpw.mx.RUnlock()
	return cpw.pool.Clone(), nil
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
}

// Drain as many events as possible before doing anything
// Otherwise, we will be hit with an event for _every_ entry in the
// directory, and end up doing an update for each one
func (cpw *CertPoolWatcher) drainEvents() {
	for {
		// sleep to let events accumulate
		time.Sleep(time.Millisecond * 50)
		select {
		case <-cpw.watcher.Events:
		default:
			return
		}
	}
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
	if err = watcher.Add(caDir); err != nil {
		return nil, err
	}
	cpw := &CertPoolWatcher{
		dir:     caDir,
		pool:    pool,
		log:     log,
		watcher: watcher,
	}
	go func() {
		for {
			select {
			case <-watcher.Events:
				cpw.drainEvents()
				cpw.update()
			case err = <-watcher.Errors:
				log.Error(err, "error watching certificate dir")
				os.Exit(1)
			}
		}
	}()
	return cpw, nil
}
