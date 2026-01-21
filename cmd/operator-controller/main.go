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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.podman.io/image/v5/types"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	apimachineryrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"pkg.package-operator.run/boxcutter/managedcache"
	ctrl "sigs.k8s.io/controller-runtime"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/action"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authorization"
	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/cache"
	catalogclient "github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/client"
	"github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager"
	cmcache "github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager/cache"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	"github.com/operator-framework/operator-controller/internal/operator-controller/finalizers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/preflights/crdupgradesafety"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/certproviders"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
	sharedcontrollers "github.com/operator-framework/operator-controller/internal/shared/controllers"
	cacheutil "github.com/operator-framework/operator-controller/internal/shared/util/cache"
	fsutil "github.com/operator-framework/operator-controller/internal/shared/util/fs"
	httputil "github.com/operator-framework/operator-controller/internal/shared/util/http"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
	"github.com/operator-framework/operator-controller/internal/shared/util/pullsecretcache"
	sautil "github.com/operator-framework/operator-controller/internal/shared/util/sa"
	"github.com/operator-framework/operator-controller/internal/shared/util/tlsprofiles"
	"github.com/operator-framework/operator-controller/internal/shared/version"
)

var (
	setupLog               = ctrl.Log.WithName("setup")
	defaultSystemNamespace = "olmv1-system"
	certWatcher            *certwatcher.CertWatcher
	cfg                    = &config{}
)

type config struct {
	metricsAddr          string
	pprofAddr            string
	certFile             string
	keyFile              string
	enableLeaderElection bool
	probeAddr            string
	cachePath            string
	systemNamespace      string
	catalogdCasDir       string
	pullCasDir           string
	globalPullSecret     string
}

type reconcilerConfigurator interface {
	Configure(cer *controllers.ClusterExtensionReconciler) error
}

type boxcutterReconcilerConfigurator struct {
	mgr                   manager.Manager
	preflights            []applier.Preflight
	regv1ManifestProvider applier.ManifestProvider
	resolver              resolve.Resolver
	imageCache            imageutil.Cache
	imagePuller           imageutil.Puller
	finalizers            crfinalizer.Finalizers
}

type helmReconcilerConfigurator struct {
	mgr                   manager.Manager
	preflights            []applier.Preflight
	regv1ManifestProvider applier.ManifestProvider
	resolver              resolve.Resolver
	imageCache            imageutil.Cache
	imagePuller           imageutil.Puller
	finalizers            crfinalizer.Finalizers
	watcher               cmcache.Watcher
}

const (
	authFilePrefix   = "operator-controller-global-pull-secrets"
	fieldOwnerPrefix = "olm.operatorframework.io"
)

// podNamespace checks whether the controller is running in a Pod vs.
// being run locally by inspecting the namespace file that gets mounted
// automatically for Pods at runtime. If that file doesn't exist, then
// return defaultSystemNamespace.
func podNamespace() string {
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return defaultSystemNamespace
	}
	return string(namespace)
}

var operatorControllerCmd = &cobra.Command{
	Use:   "operator-controller",
	Short: "operator-controller is the central component of Operator Lifecycle Manager (OLM) v1",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateMetricsFlags(); err != nil {
			return err
		}
		return run()
	},
}

var versionCommand = &cobra.Command{
	Use:   "version",
	Short: "Prints operator-controller version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.String())
	},
}

func init() {
	//create flagset, the collection of flags for this command
	flags := operatorControllerCmd.Flags()
	flags.StringVar(&cfg.metricsAddr, "metrics-bind-address", "", "The address for the metrics endpoint. Requires tls-cert and tls-key. (Default: ':8443')")
	flags.StringVar(&cfg.pprofAddr, "pprof-bind-address", "0", "The address the pprof endpoint binds to. an empty string or 0 disables pprof")
	flags.StringVar(&cfg.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flags.StringVar(&cfg.catalogdCasDir, "catalogd-cas-dir", "", "The directory of TLS certificate authorities to use for verifying HTTPS connections to the Catalogd web service.")
	flags.StringVar(&cfg.pullCasDir, "pull-cas-dir", "", "The directory of TLS certificate authorities to use for verifying HTTPS connections to image registries.")
	flags.StringVar(&cfg.certFile, "tls-cert", "", "The certificate file used for the metrics server. Required to enable the metrics server. Requires tls-key.")
	flags.StringVar(&cfg.keyFile, "tls-key", "", "The key file used for the metrics server. Required to enable the metrics server. Requires tls-cert")
	flags.BoolVar(&cfg.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flags.StringVar(&cfg.cachePath, "cache-path", "/var/cache", "The local directory path used for filesystem based caching")
	flags.StringVar(&cfg.systemNamespace, "system-namespace", "", "Configures the namespace that gets used to deploy system resources.")
	flags.StringVar(&cfg.globalPullSecret, "global-pull-secret", "", "The <namespace>/<name> of the global pull secret that is going to be used to pull bundle images.")

	//adds version sub command
	operatorControllerCmd.AddCommand(versionCommand)

	//add klog flags to flagset
	klog.InitFlags(flag.CommandLine)
	flags.AddGoFlagSet(flag.CommandLine)

	//add feature gate flags to flagset
	features.OperatorControllerFeatureGate.AddFlag(flags)

	//add TLS flags
	tlsprofiles.AddFlags(flags)

	ctrl.SetLogger(klog.NewKlogr())
}
func validateMetricsFlags() error {
	if (cfg.certFile != "" && cfg.keyFile == "") || (cfg.certFile == "" && cfg.keyFile != "") {
		setupLog.Error(errors.New("missing TLS configuration"),
			"tls-cert and tls-key flags must be used together",
			"certFile", cfg.certFile, "keyFile", cfg.keyFile)
		return fmt.Errorf("unable to configure TLS certificates: tls-cert and tls-key flags must be used together")
	}

	if cfg.metricsAddr != "" && cfg.certFile == "" && cfg.keyFile == "" {
		setupLog.Error(errors.New("invalid metrics configuration"),
			"metrics-bind-address requires tls-cert and tls-key flags to be set",
			"metricsAddr", cfg.metricsAddr, "certFile", cfg.certFile, "keyFile", cfg.keyFile)
		return fmt.Errorf("metrics-bind-address requires tls-cert and tls-key flags to be set")
	}

	if cfg.certFile != "" && cfg.keyFile != "" && cfg.metricsAddr == "" {
		cfg.metricsAddr = ":8443"
	}
	return nil
}
func run() error {
	setupLog.Info("starting up the controller", "version info", version.String())

	// log feature gate status after parsing flags and setting up logger
	features.LogFeatureGateStates(setupLog, features.OperatorControllerFeatureGate)

	authFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%s.json", authFilePrefix, apimachineryrand.String(8)))
	var globalPullSecretKey *k8stypes.NamespacedName
	if cfg.globalPullSecret != "" {
		secretParts := strings.Split(cfg.globalPullSecret, "/")
		if len(secretParts) != 2 {
			err := fmt.Errorf("incorrect number of components")
			setupLog.Error(err, "Value of global-pull-secret should be of the format <namespace>/<name>")
			return err
		}
		globalPullSecretKey = &k8stypes.NamespacedName{Name: secretParts[1], Namespace: secretParts[0]}
	}

	if cfg.systemNamespace == "" {
		cfg.systemNamespace = podNamespace()
	}

	setupLog.Info("set up manager")
	cacheOptions := crcache.Options{
		ByObject: map[client.Object]crcache.ByObject{
			&ocv1.ClusterExtension{}:     {Label: k8slabels.Everything()},
			&ocv1.ClusterCatalog{}:       {Label: k8slabels.Everything()},
			&rbacv1.ClusterRole{}:        {Label: k8slabels.Everything()},
			&rbacv1.ClusterRoleBinding{}: {Label: k8slabels.Everything()},
			&rbacv1.Role{}:               {Namespaces: map[string]crcache.Config{}, Label: k8slabels.Everything()},
			&rbacv1.RoleBinding{}:        {Namespaces: map[string]crcache.Config{}, Label: k8slabels.Everything()},
		},
		DefaultNamespaces: map[string]crcache.Config{
			cfg.systemNamespace: {LabelSelector: k8slabels.Everything()},
		},
		DefaultLabelSelector: k8slabels.Nothing(),
		// Memory optimization: strip managed fields and large annotations from cached objects
		DefaultTransform: cacheutil.StripAnnotations(),
	}

	if features.OperatorControllerFeatureGate.Enabled(features.BoxcutterRuntime) {
		cacheOptions.ByObject[&ocv1.ClusterExtensionRevision{}] = crcache.ByObject{
			Label: k8slabels.Everything(),
		}
	}

	saKey, err := sautil.GetServiceAccount()
	if err != nil {
		setupLog.Error(err, "Failed to extract serviceaccount from JWT")
		return err
	}
	setupLog.Info("Successfully extracted serviceaccount from JWT", "serviceaccount",
		fmt.Sprintf("%s/%s", saKey.Namespace, saKey.Name))

	err = pullsecretcache.SetupPullSecretCache(&cacheOptions, globalPullSecretKey, saKey)
	if err != nil {
		setupLog.Error(err, "Unable to setup pull-secret cache")
		return err
	}

	metricsServerOptions := server.Options{}
	if len(cfg.certFile) > 0 && len(cfg.keyFile) > 0 {
		setupLog.Info("Starting metrics server with TLS enabled", "addr", cfg.metricsAddr, "tls-cert", cfg.certFile, "tls-key", cfg.keyFile)

		metricsServerOptions.BindAddress = cfg.metricsAddr
		metricsServerOptions.SecureServing = true
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization

		// If the certificate files change, the watcher will reload them.
		var err error
		certWatcher, err = certwatcher.New(cfg.certFile, cfg.keyFile)
		if err != nil {
			setupLog.Error(err, "Failed to initialize certificate watcher")
			return err
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = certWatcher.GetCertificate
			// If the enable-http2 flag is false (the default), http/2 should be disabled
			// due to its vulnerabilities. More specifically, disabling http/2 will
			// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
			// Rapid Reset CVEs. For more information see:
			// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
			// - https://github.com/advisories/GHSA-4374-p667-p6c8
			// Besides, those CVEs are solved already; the solution is still insufficient, and we need to mitigate
			// the risks. More info https://github.com/golang/go/issues/63417
			config.NextProtos = []string{"http/1.1"}
		})
		tlsProfile, err := tlsprofiles.GetTLSConfigFunc()
		if err != nil {
			setupLog.Error(err, "failed to get TLS profile")
			return err
		}
		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, tlsProfile)
	} else {
		// Note that the metrics server is not serving if the BindAddress is set to "0".
		// Therefore, the metrics server is disabled by default. It is only enabled
		// if certFile and keyFile are provided. The intention is not allowing the metrics
		// be served with the default self-signed certificate generated by controller-runtime.
		metricsServerOptions.BindAddress = "0"

		setupLog.Info("WARNING: Metrics Server is disabled. " +
			"Metrics will not be served since the TLS certificate and key file are not provided.")
	}

	restConfig := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                        scheme.Scheme,
		Metrics:                       metricsServerOptions,
		PprofBindAddress:              cfg.pprofAddr,
		HealthProbeBindAddress:        cfg.probeAddr,
		LeaderElection:                cfg.enableLeaderElection,
		LeaderElectionID:              "9c4404e7.operatorframework.io",
		LeaderElectionReleaseOnCancel: true,
		// Recommended Leader Election values
		// https://github.com/openshift/enhancements/blob/61581dcd985130357d6e4b0e72b87ee35394bf6e/CONVENTIONS.md#handling-kube-apiserver-disruption
		LeaseDuration: ptr.To(137 * time.Second),
		RenewDeadline: ptr.To(107 * time.Second),
		RetryPeriod:   ptr.To(26 * time.Second),

		Cache: cacheOptions,
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	cpwCatalogd, err := httputil.NewCertPoolWatcher(cfg.catalogdCasDir, ctrl.Log.WithName("catalogd-ca-pool"))
	if err != nil {
		setupLog.Error(err, "unable to create catalogd-ca-pool watcher")
		return err
	}
	cpwCatalogd.Restart(os.Exit)
	if err = mgr.Add(cpwCatalogd); err != nil {
		setupLog.Error(err, "unable to add catalogd-ca-pool watcher to manager")
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

	if certWatcher != nil {
		setupLog.Info("Adding certificate watcher to manager")
		if err := mgr.Add(certWatcher); err != nil {
			setupLog.Error(err, "unable to add certificate watcher to manager")
			return err
		}
	}

	if err := fsutil.EnsureEmptyDirectory(cfg.cachePath, 0700); err != nil {
		setupLog.Error(err, "unable to ensure empty cache directory")
		return err
	}

	imageCache := imageutil.BundleCache(filepath.Join(cfg.cachePath, "unpack"))
	imagePuller := &imageutil.ContainersImagePuller{
		SourceCtxFunc: func(ctx context.Context) (*types.SystemContext, error) {
			srcContext := &types.SystemContext{
				DockerCertPath: cfg.pullCasDir,
				OCICertPath:    cfg.pullCasDir,
			}
			logger := log.FromContext(ctx)
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

	clusterExtensionFinalizers := crfinalizer.NewFinalizers()
	if err := clusterExtensionFinalizers.Register(controllers.ClusterExtensionCleanupUnpackCacheFinalizer, finalizers.FinalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		return crfinalizer.Result{}, imageCache.Delete(ctx, obj.GetName())
	})); err != nil {
		setupLog.Error(err, "unable to register finalizer", "finalizerKey", controllers.ClusterExtensionCleanupUnpackCacheFinalizer)
		return err
	}

	cl := mgr.GetClient()

	catalogsCachePath := filepath.Join(cfg.cachePath, "catalogs")
	if err := os.MkdirAll(catalogsCachePath, 0700); err != nil {
		setupLog.Error(err, "unable to create catalogs cache directory")
		return err
	}
	catalogClientBackend := cache.NewFilesystemCache(catalogsCachePath)
	catalogClient := catalogclient.New(catalogClientBackend, func() (*http.Client, error) {
		return httputil.BuildHTTPClient(cpwCatalogd)
	})

	resolver := &resolve.CatalogResolver{
		WalkCatalogsFunc: resolve.CatalogWalker(
			func(ctx context.Context, option ...client.ListOption) ([]ocv1.ClusterCatalog, error) {
				var catalogs ocv1.ClusterCatalogList
				if err := cl.List(ctx, &catalogs, option...); err != nil {
					return nil, err
				}
				return catalogs.Items, nil
			},
			catalogClient.GetPackage,
		),
		Validations: []resolve.ValidationFunc{
			resolve.NoDependencyValidation,
		},
	}

	aeClient, err := apiextensionsv1client.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create apiextensions client")
		return err
	}

	preflights := []applier.Preflight{
		crdupgradesafety.NewPreflight(aeClient.CustomResourceDefinitions()),
	}

	var ctrlBuilderOpts []controllers.ControllerBuilderOption
	if features.OperatorControllerFeatureGate.Enabled(features.BoxcutterRuntime) {
		ctrlBuilderOpts = append(ctrlBuilderOpts, controllers.WithOwns(&ocv1.ClusterExtensionRevision{}))
	}

	ceReconciler := &controllers.ClusterExtensionReconciler{
		Client: cl,
	}
	ceController, err := ceReconciler.SetupWithManager(mgr, ctrlBuilderOpts...)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterExtension")
		return err
	}

	certProvider := getCertificateProvider()
	regv1ManifestProvider := &applier.RegistryV1ManifestProvider{
		BundleRenderer:              registryv1.Renderer,
		CertificateProvider:         certProvider,
		IsWebhookSupportEnabled:     certProvider != nil,
		IsSingleOwnNamespaceEnabled: features.OperatorControllerFeatureGate.Enabled(features.SingleOwnNamespaceInstallSupport),
	}
	var cerCfg reconcilerConfigurator
	if features.OperatorControllerFeatureGate.Enabled(features.BoxcutterRuntime) {
		cerCfg = &boxcutterReconcilerConfigurator{
			mgr:                   mgr,
			preflights:            preflights,
			regv1ManifestProvider: regv1ManifestProvider,
			resolver:              resolver,
			imageCache:            imageCache,
			imagePuller:           imagePuller,
			finalizers:            clusterExtensionFinalizers,
		}
	} else {
		cerCfg = &helmReconcilerConfigurator{
			mgr:                   mgr,
			preflights:            preflights,
			regv1ManifestProvider: regv1ManifestProvider,
			resolver:              resolver,
			imageCache:            imageCache,
			imagePuller:           imagePuller,
			finalizers:            clusterExtensionFinalizers,
			watcher:               ceController,
		}
	}
	if err := cerCfg.Configure(ceReconciler); err != nil {
		setupLog.Error(err, "unable to setup lifecycler")
		return err
	}

	if err = (&controllers.ClusterCatalogReconciler{
		Client:                cl,
		CatalogCache:          catalogClientBackend,
		CatalogCachePopulator: catalogClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterCatalog")
		return err
	}

	setupLog.Info("creating SecretSyncer controller for watching secret", "Secret", cfg.globalPullSecret)
	err = (&sharedcontrollers.PullSecretReconciler{
		Client:            mgr.GetClient(),
		AuthFilePath:      authFilePath,
		SecretKey:         globalPullSecretKey,
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

	setupLog.Info("starting manager")
	ctx := ctrl.SetupSignalHandler()
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

func getCertificateProvider() render.CertificateProvider {
	if features.OperatorControllerFeatureGate.Enabled(features.WebhookProviderCertManager) {
		return certproviders.CertManagerCertificateProvider{}
	} else if features.OperatorControllerFeatureGate.Enabled(features.WebhookProviderOpenshiftServiceCA) {
		return certproviders.OpenshiftServiceCaCertificateProvider{}
	}
	return nil
}

func (c *boxcutterReconcilerConfigurator) Configure(ceReconciler *controllers.ClusterExtensionReconciler) error {
	coreClient, err := corev1client.NewForConfig(c.mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to create core client: %w", err)
	}
	cfgGetter, err := helmclient.NewActionConfigGetter(c.mgr.GetConfig(), c.mgr.GetRESTMapper(),
		helmclient.StorageDriverMapper(action.ChunkedStorageDriverMapper(coreClient, c.mgr.GetAPIReader(), cfg.systemNamespace)),
		helmclient.ClientNamespaceMapper(func(obj client.Object) (string, error) {
			ext := obj.(*ocv1.ClusterExtension)
			return ext.Spec.Namespace, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("unable to create helm action config getter: %w", err)
	}

	acg, err := action.NewWrappedActionClientGetter(cfgGetter,
		helmclient.WithFailureRollbacks(false),
	)
	if err != nil {
		return fmt.Errorf("unable to create helm action client getter: %w", err)
	}

	// Register a no-op finalizer handler for cleanup-contentmanager-cache.
	// This finalizer was added by the Helm applier for ClusterExtensions created
	// before BoxcutterRuntime was enabled. Boxcutter doesn't use contentmanager,
	// so we just need to acknowledge the finalizer to allow deletion to proceed.
	err = c.finalizers.Register(controllers.ClusterExtensionCleanupContentManagerCacheFinalizer, finalizers.FinalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		// No-op: Boxcutter doesn't use contentmanager, so no cleanup is needed
		return crfinalizer.Result{}, nil
	}))
	if err != nil {
		setupLog.Error(err, "unable to register content manager cleanup finalizer for boxcutter")
		return err
	}

	// determine if PreAuthorizer should be enabled based on feature gate
	var preAuth authorization.PreAuthorizer
	if features.OperatorControllerFeatureGate.Enabled(features.PreflightPermissions) {
		preAuth = authorization.NewRBACPreAuthorizer(c.mgr.GetClient())
	}

	// TODO: better scheme handling - which types do we want to support?
	_ = apiextensionsv1.AddToScheme(c.mgr.GetScheme())
	rg := &applier.SimpleRevisionGenerator{
		Scheme:           c.mgr.GetScheme(),
		ManifestProvider: c.regv1ManifestProvider,
	}
	appl := &applier.Boxcutter{
		Client:            c.mgr.GetClient(),
		Scheme:            c.mgr.GetScheme(),
		RevisionGenerator: rg,
		Preflights:        c.preflights,
		PreAuthorizer:     preAuth,
		FieldOwner:        fmt.Sprintf("%s/clusterextension-controller", fieldOwnerPrefix),
	}
	revisionStatesGetter := &controllers.BoxcutterRevisionStatesGetter{Reader: c.mgr.GetClient()}
	storageMigrator := &applier.BoxcutterStorageMigrator{
		Client:             c.mgr.GetClient(),
		Scheme:             c.mgr.GetScheme(),
		ActionClientGetter: acg,
		RevisionGenerator:  rg,
	}
	ceReconciler.ReconcileSteps = []controllers.ReconcileStepFunc{
		controllers.HandleFinalizers(c.finalizers),
		controllers.MigrateStorage(storageMigrator),
		controllers.RetrieveRevisionStates(revisionStatesGetter),
		controllers.ResolveBundle(c.resolver, c.mgr.GetClient()),
		controllers.UnpackBundle(c.imagePuller, c.imageCache),
		controllers.ApplyBundleWithBoxcutter(appl.Apply),
	}

	baseDiscoveryClient, err := discovery.NewDiscoveryClientForConfig(c.mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to create discovery client: %w", err)
	}

	// Wrap the discovery client with caching to reduce memory usage from repeated OpenAPI schema fetches
	discoveryClient := memory.NewMemCacheClient(baseDiscoveryClient)

	trackingCache, err := managedcache.NewTrackingCache(
		ctrl.Log.WithName("trackingCache"),
		c.mgr.GetConfig(),
		crcache.Options{
			Scheme: c.mgr.GetScheme(), Mapper: c.mgr.GetRESTMapper(),
		},
	)
	if err != nil {
		return fmt.Errorf("unable to create boxcutter tracking cache: %v", err)
	}
	if err := c.mgr.Add(trackingCache); err != nil {
		return fmt.Errorf("unable to add tracking cache to manager: %v", err)
	}

	cerCoreClient, err := corev1client.NewForConfig(c.mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to create client for ClusterExtensionRevision controller: %w", err)
	}
	cerTokenGetter := authentication.NewTokenGetter(cerCoreClient, authentication.WithExpirationDuration(1*time.Hour))

	revisionEngineFactory, err := controllers.NewDefaultRevisionEngineFactory(
		c.mgr.GetScheme(),
		trackingCache,
		discoveryClient,
		c.mgr.GetRESTMapper(),
		fieldOwnerPrefix,
		c.mgr.GetConfig(),
		cerTokenGetter,
	)
	if err != nil {
		return fmt.Errorf("unable to create revision engine factory: %w", err)
	}

	if err = (&controllers.ClusterExtensionRevisionReconciler{
		Client:                c.mgr.GetClient(),
		RevisionEngineFactory: revisionEngineFactory,
		TrackingCache:         trackingCache,
	}).SetupWithManager(c.mgr); err != nil {
		return fmt.Errorf("unable to setup ClusterExtensionRevision controller: %w", err)
	}
	return nil
}

func (c *helmReconcilerConfigurator) Configure(ceReconciler *controllers.ClusterExtensionReconciler) error {
	coreClient, err := corev1client.NewForConfig(c.mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to create core client: %w", err)
	}
	tokenGetter := authentication.NewTokenGetter(coreClient, authentication.WithExpirationDuration(1*time.Hour))
	clientRestConfigMapper := action.ServiceAccountRestConfigMapper(tokenGetter)
	if features.OperatorControllerFeatureGate.Enabled(features.SyntheticPermissions) {
		clientRestConfigMapper = action.SyntheticUserRestConfigMapper(clientRestConfigMapper)
	}

	cfgGetter, err := helmclient.NewActionConfigGetter(c.mgr.GetConfig(), c.mgr.GetRESTMapper(),
		helmclient.StorageDriverMapper(action.ChunkedStorageDriverMapper(coreClient, c.mgr.GetAPIReader(), cfg.systemNamespace)),
		helmclient.ClientNamespaceMapper(func(obj client.Object) (string, error) {
			ext := obj.(*ocv1.ClusterExtension)
			return ext.Spec.Namespace, nil
		}),
		helmclient.ClientRestConfigMapper(clientRestConfigMapper),
	)
	if err != nil {
		return fmt.Errorf("unable to create helm action config getter: %w", err)
	}

	acg, err := action.NewWrappedActionClientGetter(cfgGetter,
		helmclient.WithFailureRollbacks(false),
	)
	if err != nil {
		return fmt.Errorf("unable to create helm action client getter: %w", err)
	}

	// determine if PreAuthorizer should be enabled based on feature gate
	var preAuth authorization.PreAuthorizer
	if features.OperatorControllerFeatureGate.Enabled(features.PreflightPermissions) {
		preAuth = authorization.NewRBACPreAuthorizer(c.mgr.GetClient())
	}

	cm := contentmanager.NewManager(clientRestConfigMapper, c.mgr.GetConfig(), c.mgr.GetRESTMapper())
	err = c.finalizers.Register(controllers.ClusterExtensionCleanupContentManagerCacheFinalizer, finalizers.FinalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		ext := obj.(*ocv1.ClusterExtension)
		err := cm.Delete(ext)
		return crfinalizer.Result{}, err
	}))
	if err != nil {
		setupLog.Error(err, "unable to register content manager cleanup finalizer")
		return err
	}

	// now initialize the helmApplier, assigning the potentially nil preAuth
	appl := &applier.Helm{
		ActionClientGetter: acg,
		Preflights:         c.preflights,
		HelmChartProvider: &applier.RegistryV1HelmChartProvider{
			ManifestProvider: c.regv1ManifestProvider,
		},
		HelmReleaseToObjectsConverter: &applier.HelmReleaseToObjectsConverter{},
		PreAuthorizer:                 preAuth,
		Watcher:                       c.watcher,
		Manager:                       cm,
	}
	revisionStatesGetter := &controllers.HelmRevisionStatesGetter{ActionClientGetter: acg}
	ceReconciler.ReconcileSteps = []controllers.ReconcileStepFunc{
		controllers.HandleFinalizers(c.finalizers),
		controllers.RetrieveRevisionStates(revisionStatesGetter),
		controllers.ResolveBundle(c.resolver, c.mgr.GetClient()),
		controllers.UnpackBundle(c.imagePuller, c.imageCache),
		controllers.ApplyBundle(appl),
	}

	return nil
}

func main() {
	if err := operatorControllerCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
