/*
Copyright 2022.

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
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.podman.io/image/v5/types"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	apimachineryrand "k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	crwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	corecontrollers "github.com/operator-framework/operator-controller/internal/catalogd/controllers/core"
	"github.com/operator-framework/operator-controller/internal/catalogd/features"
	"github.com/operator-framework/operator-controller/internal/catalogd/garbagecollection"
	catalogdmetrics "github.com/operator-framework/operator-controller/internal/catalogd/metrics"
	"github.com/operator-framework/operator-controller/internal/catalogd/serverutil"
	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
	"github.com/operator-framework/operator-controller/internal/catalogd/webhook"
	sharedcontrollers "github.com/operator-framework/operator-controller/internal/shared/controllers"
	fsutil "github.com/operator-framework/operator-controller/internal/shared/util/fs"
	httputil "github.com/operator-framework/operator-controller/internal/shared/util/http"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
	"github.com/operator-framework/operator-controller/internal/shared/util/pullsecretcache"
	sautil "github.com/operator-framework/operator-controller/internal/shared/util/sa"
	"github.com/operator-framework/operator-controller/internal/shared/util/tlsprofiles"
	"github.com/operator-framework/operator-controller/internal/shared/version"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	cfg      = &config{}
)

const (
	storageDir     = "catalogs"
	authFilePrefix = "catalogd-global-pull-secret"
)

type config struct {
	metricsAddr          string
	enableLeaderElection bool
	probeAddr            string
	pprofAddr            string
	systemNamespace      string
	catalogServerAddr    string
	externalAddr         string
	cacheDir             string
	gcInterval           time.Duration
	certFile             string
	keyFile              string
	webhookPort          int
	pullCasDir           string
	globalPullSecret     string
	// Generated config
	globalPullSecretKey *k8stypes.NamespacedName
}

var catalogdCmd = &cobra.Command{
	Use:   "catalogd",
	Short: "Catalogd is a Kubernetes operator for managing operator catalogs",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateConfig(cfg); err != nil {
			return err
		}
		cmd.SilenceUsage = true
		return run(ctrl.SetupSignalHandler())
	},
}

var versionCommand = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%#v\n", version.String())
	},
}

func init() {
	// create flagset, the collection of flags for this command
	flags := catalogdCmd.Flags()
	flags.StringVar(&cfg.metricsAddr, "metrics-bind-address", "", "The address for the metrics endpoint. Requires tls-cert and tls-key. (Default: ':7443')")
	flags.StringVar(&cfg.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flags.StringVar(&cfg.pprofAddr, "pprof-bind-address", "0", "The address the pprof endpoint binds to. an empty string or 0 disables pprof")
	flags.BoolVar(&cfg.enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager")
	flags.StringVar(&cfg.systemNamespace, "system-namespace", "", "The namespace catalogd uses for internal state")
	flags.StringVar(&cfg.catalogServerAddr, "catalogs-server-addr", ":8443", "The address where catalogs' content will be accessible")
	flags.StringVar(&cfg.externalAddr, "external-address", "catalogd-service.olmv1-system.svc", "External address for http(s) server")
	flags.StringVar(&cfg.cacheDir, "cache-dir", "/var/cache/", "Directory for file based caching")
	flags.DurationVar(&cfg.gcInterval, "gc-interval", 12*time.Hour, "Garbage collection interval")
	flags.StringVar(&cfg.certFile, "tls-cert", "", "Certificate file for TLS")
	flags.StringVar(&cfg.keyFile, "tls-key", "", "Key file for TLS")
	flags.IntVar(&cfg.webhookPort, "webhook-server-port", 9443, "Webhook server port")
	flag.StringVar(&cfg.pullCasDir, "pull-cas-dir", "", "The directory of TLS certificate authoritiess to use for verifying HTTPS copullCasDirnnections to image registries.")
	flags.StringVar(&cfg.globalPullSecret, "global-pull-secret", "", "Global pull secret (<namespace>/<name>)")

	// adds version subcommand
	catalogdCmd.AddCommand(versionCommand)

	// Add other flags
	klog.InitFlags(flag.CommandLine)
	flags.AddGoFlagSet(flag.CommandLine)
	features.CatalogdFeatureGate.AddFlag(flags)
	tlsprofiles.AddFlags(flags)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(ocv1.AddToScheme(scheme))
	ctrl.SetLogger(klog.NewKlogr())
}

func main() {
	if err := catalogdCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func validateConfig(cfg *config) error {
	if (cfg.certFile != "" && cfg.keyFile == "") || (cfg.certFile == "" && cfg.keyFile != "") {
		err := fmt.Errorf("tls-cert and tls-key flags must be used together")
		setupLog.Error(err, "missing TLS configuration",
			"certFile", cfg.certFile, "keyFile", cfg.keyFile)
		return err
	}

	if cfg.metricsAddr != "" && cfg.certFile == "" && cfg.keyFile == "" {
		err := fmt.Errorf("metrics-bind-address requires tls-cert and tls-key flags")
		setupLog.Error(err, "invalid metrics configuration",
			"metricsAddr", cfg.metricsAddr, "certFile", cfg.certFile, "keyFile", cfg.keyFile)
		return err
	}

	if cfg.certFile != "" && cfg.keyFile != "" && cfg.metricsAddr == "" {
		cfg.metricsAddr = ":7443"
	}

	if cfg.globalPullSecret != "" {
		secretParts := strings.Split(cfg.globalPullSecret, "/")
		if len(secretParts) != 2 {
			err := errors.New("value of global-pull-secret should be of the format <namespace>/<name>")
			setupLog.Error(err, "incorrect number of components",
				"globalPullSecret", cfg.globalPullSecret)
			return err
		}
		cfg.globalPullSecretKey = &k8stypes.NamespacedName{Name: secretParts[1], Namespace: secretParts[0]}
	}

	return nil
}

func run(ctx context.Context) error {
	// log startup message and feature gate status
	setupLog.Info("starting up catalogd", "version info", version.String())
	features.LogFeatureGateStates(setupLog, features.CatalogdFeatureGate)
	authFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%s.json", authFilePrefix, apimachineryrand.String(8)))

	protocol := "http://"
	if cfg.certFile != "" && cfg.keyFile != "" {
		protocol = "https://"
	}
	cfg.externalAddr = protocol + cfg.externalAddr

	cw, err := certwatcher.New(cfg.certFile, cfg.keyFile)
	if err != nil {
		setupLog.Error(err, "failed to initialize certificate watcher")
		return err
	}

	tlsOpts := func(config *tls.Config) {
		config.GetCertificate = cw.GetCertificate
		// Ensure HTTP/2 is disabled by default for webhooks and metrics.
		// Disabling HTTP/2 mitigates vulnerabilities associated with:
		// - HTTP/2 Stream Cancellation (GHSA-qppj-fm5r-hxr3)
		// - HTTP/2 Rapid Reset (GHSA-4374-p667-p6c8)
		// While CVE fixes exist, they remain insufficient; disabling HTTP/2 helps reduce risks.
		// For details, see: https://github.com/kubernetes/kubernetes/issues/121197
		config.NextProtos = []string{"http/1.1"}
	}
	tlsProfile, err := tlsprofiles.GetTLSConfigFunc()
	if err != nil {
		setupLog.Error(err, "failed to get TLS profile")
		return err
	}

	// Create webhook server and configure TLS
	webhookServer := crwebhook.NewServer(crwebhook.Options{
		Port: cfg.webhookPort,
		TLSOpts: []func(*tls.Config){
			tlsOpts,
			tlsProfile,
		},
	})

	metricsServerOptions := metricsserver.Options{}
	if len(cfg.certFile) > 0 && len(cfg.keyFile) > 0 {
		setupLog.Info("Starting metrics server with TLS enabled", "addr", cfg.metricsAddr, "tls-cert", cfg.certFile, "tls-key", cfg.keyFile)

		metricsServerOptions.BindAddress = cfg.metricsAddr
		metricsServerOptions.SecureServing = true
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, tlsOpts, tlsProfile)
	} else {
		// Note that the metrics server is not serving if the BindAddress is set to "0".
		// Therefore, the metrics server is disabled by default. It is only enabled
		// if certFile and keyFile are provided. The intention is not allowing the metrics
		// be served with the default self-signed certificate generated by controller-runtime.
		metricsServerOptions.BindAddress = "0"
		setupLog.Info("WARNING: Metrics Server is disabled. " +
			"Metrics will not be served since the TLS certificate and key file are not provided.")
	}

	cacheOptions := crcache.Options{
		ByObject: map[client.Object]crcache.ByObject{},
	}

	saKey, err := sautil.GetServiceAccount()
	if err != nil {
		setupLog.Error(err, "Failed to extract serviceaccount from JWT")
		return err
	}
	setupLog.Info("Successfully extracted serviceaccount from JWT", "serviceaccount",
		fmt.Sprintf("%s/%s", saKey.Namespace, saKey.Name))

	err = pullsecretcache.SetupPullSecretCache(&cacheOptions, cfg.globalPullSecretKey, saKey)
	if err != nil {
		setupLog.Error(err, "Unable to setup pull-secret cache")
		return err
	}

	// Create manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                        scheme,
		Metrics:                       metricsServerOptions,
		PprofBindAddress:              cfg.pprofAddr,
		HealthProbeBindAddress:        cfg.probeAddr,
		LeaderElection:                cfg.enableLeaderElection,
		LeaderElectionID:              "catalogd-operator-lock",
		LeaderElectionReleaseOnCancel: true,
		// Recommended Leader Election values
		// https://github.com/openshift/enhancements/blob/61581dcd985130357d6e4b0e72b87ee35394bf6e/CONVENTIONS.md#handling-kube-apiserver-disruption
		LeaseDuration: ptr.To(137 * time.Second),
		RenewDeadline: ptr.To(107 * time.Second),
		RetryPeriod:   ptr.To(26 * time.Second),

		WebhookServer: webhookServer,
		Cache:         cacheOptions,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		return err
	}

	// Add the certificate watcher to the manager
	err = mgr.Add(cw)
	if err != nil {
		setupLog.Error(err, "unable to add certificate watcher to manager")
		return err
	}

	// This watches the pullCasDir and the SSL_CERT_DIR, and SSL_CERT_FILE for changes
	cpwPull, err := httputil.NewCertPoolWatcher(cfg.pullCasDir, ctrl.Log.WithName("pull-ca-pool"))
	if err != nil {
		setupLog.Error(err, "unable to create pull-ca-pool watcher")
		return err
	}
	cpwPull.Restart(os.Exit)
	if err = mgr.Add(cpwPull); err != nil {
		setupLog.Error(err, "unable to add pull-ca-pool watcher to manager")
		return err
	}

	if cfg.systemNamespace == "" {
		cfg.systemNamespace = podNamespace()
	}

	if err := fsutil.EnsureEmptyDirectory(cfg.cacheDir, 0700); err != nil {
		setupLog.Error(err, "unable to ensure empty cache directory")
		return err
	}

	unpackCacheBasePath := filepath.Join(cfg.cacheDir, "unpack")
	if err := os.MkdirAll(unpackCacheBasePath, 0770); err != nil {
		setupLog.Error(err, "unable to create cache directory for unpacking")
		return err
	}

	imageCache := imageutil.CatalogCache(unpackCacheBasePath)
	imagePuller := &imageutil.ContainersImagePuller{
		SourceCtxFunc: func(ctx context.Context) (*types.SystemContext, error) {
			logger := log.FromContext(ctx)
			srcContext := &types.SystemContext{
				DockerCertPath: cfg.pullCasDir,
				OCICertPath:    cfg.pullCasDir,
			}
			if _, err := os.Stat(authFilePath); err == nil {
				logger.Info("using available authentication information for pulling image")
				srcContext.AuthFilePath = authFilePath
			} else if os.IsNotExist(err) {
				logger.Info("no authentication information found for pulling image, proceeding without auth")
			} else {
				return nil, fmt.Errorf("could not stat auth file, error: %w", err)
			}
			return srcContext, nil
		},
	}

	var localStorage storage.Instance
	metrics.Registry.MustRegister(catalogdmetrics.RequestDurationMetric)

	storeDir := filepath.Join(cfg.cacheDir, storageDir)
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		setupLog.Error(err, "unable to create storage directory for catalogs")
		return err
	}

	baseStorageURL, err := url.Parse(fmt.Sprintf("%s/catalogs/", cfg.externalAddr))
	if err != nil {
		setupLog.Error(err, "unable to create base storage URL")
		return err
	}

	localStorage = &storage.LocalDirV1{
		RootDir:            storeDir,
		RootURL:            baseStorageURL,
		EnableMetasHandler: features.CatalogdFeatureGate.Enabled(features.APIV1MetasHandler),
	}

	// Config for the catalogd web server
	catalogServerConfig := serverutil.CatalogServerConfig{
		ExternalAddr: cfg.externalAddr,
		CatalogAddr:  cfg.catalogServerAddr,
		CertFile:     cfg.certFile,
		KeyFile:      cfg.keyFile,
		LocalStorage: localStorage,
	}

	err = serverutil.AddCatalogServerToManager(mgr, catalogServerConfig, cw)
	if err != nil {
		setupLog.Error(err, "unable to configure catalog server")
		return err
	}

	if err = (&corecontrollers.ClusterCatalogReconciler{
		Client:      mgr.GetClient(),
		ImageCache:  imageCache,
		ImagePuller: imagePuller,
		Storage:     localStorage,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterCatalog")
		return err
	}

	setupLog.Info("creating SecretSyncer controller for watching secret", "Secret", cfg.globalPullSecret)
	err = (&sharedcontrollers.PullSecretReconciler{
		Client:            mgr.GetClient(),
		AuthFilePath:      authFilePath,
		SecretKey:         cfg.globalPullSecretKey,
		ServiceAccountKey: saKey,
	}).SetupWithManager(mgr)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SecretSyncer")
		return err
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	metaClient, err := metadata.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to setup client for garbage collection")
		return err
	}

	gc := &garbagecollection.GarbageCollector{
		CachePath:      unpackCacheBasePath,
		Logger:         ctrl.Log.WithName("garbage-collector"),
		MetadataClient: metaClient,
		Interval:       cfg.gcInterval,
	}
	if err := mgr.Add(gc); err != nil {
		setupLog.Error(err, "unable to add garbage collector to manager")
		return err
	}

	// mutating webhook that labels ClusterCatalogs with name label
	if err = (&webhook.ClusterCatalog{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ClusterCatalog")
		return err
	}

	setupLog.Info("starting mutating webhook manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}
	if err := os.Remove(authFilePath); err != nil {
		setupLog.Error(err, "failed to cleanup temporary auth file")
		return err
	}
	return nil
}

func podNamespace() string {
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "olmv1-system"
	}
	return string(namespace)
}
