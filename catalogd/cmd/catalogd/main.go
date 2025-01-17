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
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/image/v5/types"
	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	apimachineryrand "k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	crwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	catalogdv1 "github.com/operator-framework/operator-controller/catalogd/api/v1"
	corecontrollers "github.com/operator-framework/operator-controller/catalogd/internal/controllers/core"
	"github.com/operator-framework/operator-controller/catalogd/internal/features"
	"github.com/operator-framework/operator-controller/catalogd/internal/garbagecollection"
	catalogdmetrics "github.com/operator-framework/operator-controller/catalogd/internal/metrics"
	"github.com/operator-framework/operator-controller/catalogd/internal/serverutil"
	"github.com/operator-framework/operator-controller/catalogd/internal/source"
	"github.com/operator-framework/operator-controller/catalogd/internal/storage"
	"github.com/operator-framework/operator-controller/catalogd/internal/version"
	"github.com/operator-framework/operator-controller/catalogd/internal/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const (
	storageDir     = "catalogs"
	authFilePrefix = "catalogd-global-pull-secret"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(catalogdv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		pprofAddr            string
		catalogdVersion      bool
		systemNamespace      string
		catalogServerAddr    string
		externalAddr         string
		cacheDir             string
		gcInterval           time.Duration
		certFile             string
		keyFile              string
		webhookPort          int
		caCertDir            string
		globalPullSecret     string
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "", "The address for the metrics endpoint. Requires tls-cert and tls-key. (Default: ':7443')")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&pprofAddr, "pprof-bind-address", "0", "The address the pprof endpoint binds to. an empty string or 0 disables pprof")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&systemNamespace, "system-namespace", "", "The namespace catalogd uses for internal state, configuration, and workloads")
	flag.StringVar(&catalogServerAddr, "catalogs-server-addr", ":8443", "The address where the unpacked catalogs' content will be accessible")
	flag.StringVar(&externalAddr, "external-address", "catalogd-service.olmv1-system.svc", "The external address at which the http(s) server is reachable.")
	flag.StringVar(&cacheDir, "cache-dir", "/var/cache/", "The directory in the filesystem that catalogd will use for file based caching")
	flag.BoolVar(&catalogdVersion, "version", false, "print the catalogd version and exit")
	flag.DurationVar(&gcInterval, "gc-interval", 12*time.Hour, "interval in which garbage collection should be run against the catalog content cache")
	flag.StringVar(&certFile, "tls-cert", "", "The certificate file used for serving catalog and metrics. Required to enable the metrics server. Requires tls-key.")
	flag.StringVar(&keyFile, "tls-key", "", "The key file used for serving catalog contents and metrics. Required to enable the metrics server. Requires tls-cert.")
	flag.IntVar(&webhookPort, "webhook-server-port", 9443, "The port that the mutating webhook server serves at.")
	flag.StringVar(&caCertDir, "ca-certs-dir", "", "The directory of CA certificate to use for verifying HTTPS connections to image registries.")
	flag.StringVar(&globalPullSecret, "global-pull-secret", "", "The <namespace>/<name> of the global pull secret that is going to be used to pull bundle images.")

	klog.InitFlags(flag.CommandLine)

	// Combine both flagsets and parse them
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	features.CatalogdFeatureGate.AddFlag(pflag.CommandLine)
	pflag.Parse()

	if catalogdVersion {
		fmt.Printf("%#v\n", version.Version())
		os.Exit(0)
	}

	ctrl.SetLogger(textlogger.NewLogger(textlogger.NewConfig()))

	authFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%s.json", authFilePrefix, apimachineryrand.String(8)))
	var globalPullSecretKey *k8stypes.NamespacedName
	if globalPullSecret != "" {
		secretParts := strings.Split(globalPullSecret, "/")
		if len(secretParts) != 2 {
			setupLog.Error(fmt.Errorf("incorrect number of components"), "value of global-pull-secret should be of the format <namespace>/<name>")
			os.Exit(1)
		}
		globalPullSecretKey = &k8stypes.NamespacedName{Name: secretParts[1], Namespace: secretParts[0]}
	}

	if (certFile != "" && keyFile == "") || (certFile == "" && keyFile != "") {
		setupLog.Error(nil, "unable to configure TLS certificates: tls-cert and tls-key flags must be used together")
		os.Exit(1)
	}

	if metricsAddr != "" && certFile == "" && keyFile == "" {
		setupLog.Error(nil, "metrics-bind-address requires tls-cert and tls-key flags to be set")
		os.Exit(1)
	}

	if certFile != "" && keyFile != "" && metricsAddr == "" {
		metricsAddr = ":7443"
	}

	protocol := "http://"
	if certFile != "" && keyFile != "" {
		protocol = "https://"
	}
	externalAddr = protocol + externalAddr

	cfg := ctrl.GetConfigOrDie()

	cw, err := certwatcher.New(certFile, keyFile)
	if err != nil {
		log.Fatalf("Failed to initialize certificate watcher: %v", err)
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

	// Create webhook server and configure TLS
	webhookServer := crwebhook.NewServer(crwebhook.Options{
		Port: webhookPort,
		TLSOpts: []func(*tls.Config){
			tlsOpts,
		},
	})

	metricsServerOptions := metricsserver.Options{}
	if len(certFile) > 0 && len(keyFile) > 0 {
		setupLog.Info("Starting metrics server with TLS enabled", "addr", metricsAddr, "tls-cert", certFile, "tls-key", keyFile)

		metricsServerOptions.BindAddress = metricsAddr
		metricsServerOptions.SecureServing = true
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, tlsOpts)
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
	if globalPullSecretKey != nil {
		cacheOptions.ByObject[&corev1.Secret{}] = crcache.ByObject{
			Namespaces: map[string]crcache.Config{
				globalPullSecretKey.Namespace: {
					LabelSelector: k8slabels.Everything(),
					FieldSelector: fields.SelectorFromSet(map[string]string{
						"metadata.name": globalPullSecretKey.Name,
					}),
				},
			},
		}
	}

	// Create manager
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		PprofBindAddress:       pprofAddr,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "catalogd-operator-lock",
		WebhookServer:          webhookServer,
		Cache:                  cacheOptions,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Add the certificate watcher to the manager
	err = mgr.Add(cw)
	if err != nil {
		setupLog.Error(err, "unable to add certificate watcher to manager")
		os.Exit(1)
	}

	if systemNamespace == "" {
		systemNamespace = podNamespace()
	}

	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		setupLog.Error(err, "unable to create cache directory")
		os.Exit(1)
	}

	unpackCacheBasePath := filepath.Join(cacheDir, source.UnpackCacheDir)
	if err := os.MkdirAll(unpackCacheBasePath, 0770); err != nil {
		setupLog.Error(err, "unable to create cache directory for unpacking")
		os.Exit(1)
	}
	unpacker := &source.ContainersImageRegistry{
		BaseCachePath: unpackCacheBasePath,
		SourceContextFunc: func(logger logr.Logger) (*types.SystemContext, error) {
			srcContext := &types.SystemContext{
				DockerCertPath: caCertDir,
				OCICertPath:    caCertDir,
			}
			if _, err := os.Stat(authFilePath); err == nil && globalPullSecretKey != nil {
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

	storeDir := filepath.Join(cacheDir, storageDir)
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		setupLog.Error(err, "unable to create storage directory for catalogs")
		os.Exit(1)
	}

	baseStorageURL, err := url.Parse(fmt.Sprintf("%s/catalogs/", externalAddr))
	if err != nil {
		setupLog.Error(err, "unable to create base storage URL")
		os.Exit(1)
	}

	localStorage = &storage.LocalDirV1{
		RootDir:            storeDir,
		RootURL:            baseStorageURL,
		EnableQueryHandler: features.CatalogdFeatureGate.Enabled(features.APIV1QueryHandler),
	}

	// Config for the catalogd web server
	catalogServerConfig := serverutil.CatalogServerConfig{
		ExternalAddr: externalAddr,
		CatalogAddr:  catalogServerAddr,
		CertFile:     certFile,
		KeyFile:      keyFile,
		LocalStorage: localStorage,
	}

	err = serverutil.AddCatalogServerToManager(mgr, catalogServerConfig, cw)
	if err != nil {
		setupLog.Error(err, "unable to configure catalog server")
		os.Exit(1)
	}

	if err = (&corecontrollers.ClusterCatalogReconciler{
		Client:   mgr.GetClient(),
		Unpacker: unpacker,
		Storage:  localStorage,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterCatalog")
		os.Exit(1)
	}

	if globalPullSecretKey != nil {
		setupLog.Info("creating SecretSyncer controller for watching secret", "Secret", globalPullSecret)
		err := (&corecontrollers.PullSecretReconciler{
			Client:       mgr.GetClient(),
			AuthFilePath: authFilePath,
			SecretKey:    *globalPullSecretKey,
		}).SetupWithManager(mgr)
		if err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "SecretSyncer")
			os.Exit(1)
		}
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	metaClient, err := metadata.NewForConfig(cfg)
	if err != nil {
		setupLog.Error(err, "unable to setup client for garbage collection")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()
	gc := &garbagecollection.GarbageCollector{
		CachePath:      unpackCacheBasePath,
		Logger:         ctrl.Log.WithName("garbage-collector"),
		MetadataClient: metaClient,
		Interval:       gcInterval,
	}
	if err := mgr.Add(gc); err != nil {
		setupLog.Error(err, "unable to add garbage collector to manager")
		os.Exit(1)
	}

	// mutating webhook that labels ClusterCatalogs with name label
	if err = (&webhook.ClusterCatalog{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ClusterCatalog")
		os.Exit(1)
	}

	setupLog.Info("starting mutating webhook manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
	if err := os.Remove(authFilePath); err != nil {
		setupLog.Error(err, "failed to cleanup temporary auth file")
		os.Exit(1)
	}
}

func podNamespace() string {
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "olmv1-system"
	}
	return string(namespace)
}
