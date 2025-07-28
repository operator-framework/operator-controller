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

	"github.com/containers/image/v5/types"
	"github.com/spf13/cobra"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	apimachineryrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/discovery"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/managedcache"
	"pkg.package-operator.run/boxcutter/ownerhandling"
	"pkg.package-operator.run/boxcutter/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	"github.com/operator-framework/operator-controller/internal/operator-controller/finalizers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/preflights/crdupgradesafety"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/certproviders"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
	sharedcontrollers "github.com/operator-framework/operator-controller/internal/shared/controllers"
	fsutil "github.com/operator-framework/operator-controller/internal/shared/util/fs"
	httputil "github.com/operator-framework/operator-controller/internal/shared/util/http"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
	"github.com/operator-framework/operator-controller/internal/shared/util/pullsecretcache"
	sautil "github.com/operator-framework/operator-controller/internal/shared/util/sa"
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

const authFilePrefix = "operator-controller-global-pull-secrets"

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

	coreClient, err := corev1client.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create core client")
		return err
	}
	tokenGetter := authentication.NewTokenGetter(coreClient, authentication.WithExpirationDuration(1*time.Hour))
	clientRestConfigMapper := action.ServiceAccountRestConfigMapper(tokenGetter)
	if features.OperatorControllerFeatureGate.Enabled(features.SyntheticPermissions) {
		clientRestConfigMapper = action.SyntheticUserRestConfigMapper(clientRestConfigMapper)
	}

	cfgGetter, err := helmclient.NewActionConfigGetter(mgr.GetConfig(), mgr.GetRESTMapper(),
		helmclient.StorageDriverMapper(action.ChunkedStorageDriverMapper(coreClient, mgr.GetAPIReader(), cfg.systemNamespace)),
		helmclient.ClientNamespaceMapper(func(obj client.Object) (string, error) {
			ext := obj.(*ocv1.ClusterExtension)
			return ext.Spec.Namespace, nil
		}),
		helmclient.ClientRestConfigMapper(clientRestConfigMapper),
	)
	if err != nil {
		setupLog.Error(err, "unable to config for creating helm client")
		return err
	}

	acg, err := action.NewWrappedActionClientGetter(cfgGetter,
		helmclient.WithFailureRollbacks(false),
	)
	if err != nil {
		setupLog.Error(err, "unable to create helm client")
		return err
	}

	certPoolWatcher, err := httputil.NewCertPoolWatcher(cfg.catalogdCasDir, ctrl.Log.WithName("cert-pool"))
	if err != nil {
		setupLog.Error(err, "unable to create CA certificate pool")
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
		return httputil.BuildHTTPClient(certPoolWatcher)
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

	// determine if PreAuthorizer should be enabled based on feature gate
	var preAuth authorization.PreAuthorizer
	if features.OperatorControllerFeatureGate.Enabled(features.PreflightPermissions) {
		preAuth = authorization.NewRBACPreAuthorizer(mgr.GetClient())
	}

	// create applier
	var ctrlBuilderOpts []controllers.ControllerBuilderOption
	var extApplier controllers.Applier

	if features.OperatorControllerFeatureGate.Enabled(features.BoxcutterRuntime) {
		// TODO: add support for preflight checks
		// TODO: better scheme handling - which types do we want to support?
		_ = apiextensionsv1.AddToScheme(mgr.GetScheme())
		extApplier = &applier.Boxcutter{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			RevisionGenerator: &applier.SimpleRevisionGenerator{
				Scheme: mgr.GetScheme(),
				BundleRenderer: &applier.RegistryV1BundleRenderer{
					BundleRenderer: registryv1.Renderer,
				},
			},
		}
		ctrlBuilderOpts = append(ctrlBuilderOpts, controllers.WithOwns(&ocv1.ClusterExtensionRevision{}))
	} else {
		// now initialize the helmApplier, assigning the potentially nil preAuth
		certProvider := getCertificateProvider()
		extApplier = &applier.Helm{
			ActionClientGetter: acg,
			Preflights:         preflights,
			BundleToHelmChartConverter: &convert.BundleToHelmChartConverter{
				BundleRenderer:          registryv1.Renderer,
				CertificateProvider:     certProvider,
				IsWebhookSupportEnabled: certProvider != nil,
			},
			PreAuthorizer: preAuth,
		}
	}

	cm := contentmanager.NewManager(clientRestConfigMapper, mgr.GetConfig(), mgr.GetRESTMapper())
	err = clusterExtensionFinalizers.Register(controllers.ClusterExtensionCleanupContentManagerCacheFinalizer, finalizers.FinalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		ext := obj.(*ocv1.ClusterExtension)
		err := cm.Delete(ext)
		return crfinalizer.Result{}, err
	}))
	if err != nil {
		setupLog.Error(err, "unable to register content manager cleanup finalizer")
		return err
	}

	if err = (&controllers.ClusterExtensionReconciler{
		Client:                cl,
		Resolver:              resolver,
		ImageCache:            imageCache,
		ImagePuller:           imagePuller,
		Applier:               extApplier,
		InstalledBundleGetter: &controllers.DefaultInstalledBundleGetter{ActionClientGetter: acg},
		Finalizers:            clusterExtensionFinalizers,
		Manager:               cm,
	}).SetupWithManager(mgr, ctrlBuilderOpts...); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterExtension")
		return err
	}

	if features.OperatorControllerFeatureGate.Enabled(features.BoxcutterRuntime) {
		// Boxcutter
		const (
			boxcutterSystemPrefixFieldOwner = "olm.operatorframework.io"
		)

		discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
		if err != nil {
			setupLog.Error(err, "unable to create discovery client")
			return err
		}

		trackingCache, err := managedcache.NewTrackingCache(
			ctrl.Log.WithName("accessmanager"),
			restConfig,
			crcache.Options{
				Scheme: mgr.GetScheme(), Mapper: mgr.GetRESTMapper(),
			},
		)
		if err != nil {
			setupLog.Error(err, "unable to create boxcutter tracking cache")
		}

		if err = (&controllers.ClusterExtensionRevisionReconciler{
			Client: cl,
			RevisionEngine: machinery.NewRevisionEngine(
				machinery.NewPhaseEngine(
					machinery.NewObjectEngine(
						mgr.GetScheme(), trackingCache, mgr.GetClient(),
						ownerhandling.NewNative(mgr.GetScheme()),
						machinery.NewComparator(ownerhandling.NewNative(mgr.GetScheme()), discoveryClient, mgr.GetScheme(), boxcutterSystemPrefixFieldOwner),
						boxcutterSystemPrefixFieldOwner, boxcutterSystemPrefixFieldOwner,
					),
					validation.NewClusterPhaseValidator(mgr.GetRESTMapper(), mgr.GetClient()),
				),
				validation.NewRevisionValidator(), mgr.GetClient(),
			),
		}).SetupWithManager(mgr, trackingCache); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ClusterExtensionRevision")
			return err
		}
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

func main() {
	if err := operatorControllerCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
