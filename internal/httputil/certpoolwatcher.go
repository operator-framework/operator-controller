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
		watcher: watcher,
	}
	go func() {
		for {
			select {
			case <-watcher.Events:
				func() {
					// drain as many events as possible before doing anything
					func() {
						for {
							time.Sleep(time.Millisecond * 100)
							select {
							case <-watcher.Events:
							default:
								return
							}
						}
					}()
					log.Info("updating certificate pool")
					pool, err := NewCertPool(caDir, log)
					if err != nil {
						log.Error(err, "error updating certificate pool")
						os.Exit(1)
					}
					cpw.mx.Lock()
					defer cpw.mx.Unlock()
					cpw.pool = pool
				}()
			case err = <-watcher.Errors:
				log.Error(err, "error watching certificate dir")
				os.Exit(1)
			}
		}
	}()
	return cpw, nil
}
