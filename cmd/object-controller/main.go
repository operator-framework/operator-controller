/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"pkg.package-operator.run/boxcutter/managedcache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/object-controller/scheme"
	cacheutil "github.com/operator-framework/operator-controller/internal/shared/util/cache"
	"github.com/operator-framework/operator-controller/internal/shared/util/tlsprofiles"
	"github.com/operator-framework/operator-controller/internal/shared/version"
)

type config struct {
	metricsAddr          string
	pprofAddr            string
	probeAddr            string
	certFile             string
	keyFile              string
	enableLeaderElection bool
}

func newCommand() *cobra.Command {
	cfg := &config{}
	cmd := &cobra.Command{
		Use:   "object-controller",
		Short: "Manage Kubernetes objects through ClusterObjectSets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cfg.validate(); err != nil {
				return err
			}
			restConfig, err := ctrl.GetConfig()
			if err != nil {
				return err
			}
			mgr, err := newManager(cfg, restConfig)
			if err != nil {
				return err
			}
			ctrl.Log.WithName("setup").Info("starting object-controller", "version info", version.String())
			return mgr.Start(cmd.Context())
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&cfg.metricsAddr, "metrics-bind-address", "", "The metrics endpoint address. Requires tls-cert and tls-key. (Default: ':8443')")
	flags.StringVar(&cfg.pprofAddr, "pprof-bind-address", "0", "The pprof endpoint address. An empty string or 0 disables pprof.")
	flags.StringVar(&cfg.probeAddr, "health-probe-bind-address", ":8081", "The health probe endpoint address.")
	flags.StringVar(&cfg.certFile, "tls-cert", "", "The certificate file for the metrics server. Requires tls-key.")
	flags.StringVar(&cfg.keyFile, "tls-key", "", "The key file for the metrics server. Requires tls-cert.")
	flags.BoolVar(&cfg.enableLeaderElection, "leader-elect", false, "Enable leader election for the controller manager.")
	logFlags := flag.NewFlagSet("logging", flag.ContinueOnError)
	klog.InitFlags(logFlags)
	flags.AddGoFlagSet(logFlags)
	tlsprofiles.AddFlags(flags)
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print object-controller version information",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println(version.String())
		},
	})
	return cmd
}

func (c *config) validate() error {
	if (c.certFile == "") != (c.keyFile == "") {
		return fmt.Errorf("tls-cert and tls-key flags must be used together")
	}
	if c.metricsAddr != "" && c.certFile == "" {
		return fmt.Errorf("metrics-bind-address requires tls-cert and tls-key flags to be set")
	}
	if c.certFile != "" && c.metricsAddr == "" {
		c.metricsAddr = ":8443"
	}
	return nil
}

func newManager(cfg *config, restConfig *rest.Config) (manager.Manager, error) {
	metricsOptions := server.Options{BindAddress: "0"}
	var certWatcher *certwatcher.CertWatcher
	if cfg.certFile != "" {
		var err error
		certWatcher, err = certwatcher.New(cfg.certFile, cfg.keyFile)
		if err != nil {
			return nil, fmt.Errorf("initializing certificate watcher: %w", err)
		}
		tlsProfile, err := tlsprofiles.GetTLSConfigFunc()
		if err != nil {
			return nil, fmt.Errorf("getting TLS profile: %w", err)
		}
		metricsOptions = server.Options{
			BindAddress:    cfg.metricsAddr,
			SecureServing:  true,
			FilterProvider: filters.WithAuthenticationAndAuthorization,
			TLSOpts: []func(*tls.Config){
				func(c *tls.Config) {
					c.GetCertificate = certWatcher.GetCertificate
					c.NextProtos = []string{"http/1.1"}
				},
				tlsProfile,
			},
		}
	}
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                        scheme.Scheme,
		Metrics:                       metricsOptions,
		PprofBindAddress:              cfg.pprofAddr,
		HealthProbeBindAddress:        cfg.probeAddr,
		LeaderElection:                cfg.enableLeaderElection,
		LeaderElectionID:              "object-controller-lock.olm.operatorframework.io",
		LeaderElectionReleaseOnCancel: true,
		LeaseDuration:                 ptr.To(137 * time.Second),
		RenewDeadline:                 ptr.To(107 * time.Second),
		RetryPeriod:                   ptr.To(26 * time.Second),
		Cache: cache.Options{
			ByObject:                    map[client.Object]cache.ByObject{&ocv1.ClusterObjectSet{}: {}},
			ReaderFailOnMissingInformer: true,
			DefaultTransform:            cacheutil.StripAnnotations(),
		},
		// References can point to immutable Secrets in any namespace. Read them
		// directly, without caching unrelated cluster Secrets or requiring a system namespace.
		Client: client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{&corev1.Secret{}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("creating manager: %w", err)
	}
	if certWatcher != nil {
		if err := mgr.Add(certWatcher); err != nil {
			return nil, fmt.Errorf("adding certificate watcher: %w", err)
		}
	}
	trackingCache, err := managedcache.NewTrackingCache(
		ctrl.Log.WithName("trackingCache"), mgr.GetConfig(),
		cache.Options{Scheme: mgr.GetScheme(), Mapper: mgr.GetRESTMapper()},
	)
	if err != nil {
		return nil, fmt.Errorf("creating tracking cache: %w", err)
	}
	if err := mgr.Add(trackingCache); err != nil {
		return nil, fmt.Errorf("adding tracking cache: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("creating discovery client: %w", err)
	}
	// Keep the field owner prefix unchanged so existing objects can be reconciled after migration.
	factory, err := controllers.NewDefaultRevisionEngineFactory(
		mgr.GetScheme(), trackingCache, memory.NewMemCacheClient(discoveryClient),
		mgr.GetRESTMapper(), "olm.operatorframework.io", mgr.GetConfig(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating revision engine factory: %w", err)
	}
	if err := (&controllers.ClusterObjectSetReconciler{
		Client: mgr.GetClient(), RevisionEngineFactory: factory, TrackingCache: trackingCache,
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setting up ClusterObjectSet controller: %w", err)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return nil, err
	}
	return mgr, nil
}

func main() {
	ctrl.SetLogger(klog.NewKlogr())
	if err := newCommand().ExecuteContext(ctrl.SetupSignalHandler()); err != nil {
		os.Exit(1)
	}
}
