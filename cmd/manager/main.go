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
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/image/v5/types"
	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/apimachinery/pkg/fields"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	apimachineryrand "k8s.io/apimachinery/pkg/util/rand"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/action"
	"github.com/operator-framework/operator-controller/internal/applier"
	"github.com/operator-framework/operator-controller/internal/authentication"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/cache"
	catalogclient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	"github.com/operator-framework/operator-controller/internal/contentmanager"
	"github.com/operator-framework/operator-controller/internal/controllers"
	"github.com/operator-framework/operator-controller/internal/features"
	"github.com/operator-framework/operator-controller/internal/finalizers"
	"github.com/operator-framework/operator-controller/internal/httputil"
	"github.com/operator-framework/operator-controller/internal/resolve"
	"github.com/operator-framework/operator-controller/internal/rukpak/preflights/crdupgradesafety"
	"github.com/operator-framework/operator-controller/internal/rukpak/source"
	"github.com/operator-framework/operator-controller/internal/scheme"
	"github.com/operator-framework/operator-controller/internal/version"
)

var (
	setupLog               = ctrl.Log.WithName("setup")
	defaultSystemNamespace = "olmv1-system"
)

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

func main() {
	var (
		metricsAddr               string
		enableLeaderElection      bool
		probeAddr                 string
		cachePath                 string
		operatorControllerVersion bool
		systemNamespace           string
		caCertDir                 string
		globalPullSecret          string
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&caCertDir, "ca-certs-dir", "", "The directory of TLS certificate to use for verifying HTTPS connections to the Catalogd and docker-registry web servers.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&cachePath, "cache-path", "/var/cache", "The local directory path used for filesystem based caching")
	flag.BoolVar(&operatorControllerVersion, "version", false, "Prints operator-controller version information")
	flag.StringVar(&systemNamespace, "system-namespace", "", "Configures the namespace that gets used to deploy system resources.")
	flag.StringVar(&globalPullSecret, "global-pull-secret", "", "The <namespace>/<name> of the global pull secret that is going to be used to pull bundle images.")

	klog.InitFlags(flag.CommandLine)

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	features.OperatorControllerFeatureGate.AddFlag(pflag.CommandLine)
	pflag.Parse()

	if operatorControllerVersion {
		fmt.Println(version.String())
		os.Exit(0)
	}

	ctrl.SetLogger(textlogger.NewLogger(textlogger.NewConfig()))

	setupLog.Info("starting up the controller", "version info", version.String())

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

	if systemNamespace == "" {
		systemNamespace = podNamespace()
	}

	setupLog.Info("set up manager")
	cacheOptions := crcache.Options{
		ByObject: map[client.Object]crcache.ByObject{
			&ocv1alpha1.ClusterExtension{}: {Label: k8slabels.Everything()},
			&catalogd.ClusterCatalog{}:     {Label: k8slabels.Everything()},
		},
		DefaultNamespaces: map[string]crcache.Config{
			systemNamespace: {LabelSelector: k8slabels.Everything()},
		},
		DefaultLabelSelector: k8slabels.Nothing(),
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
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme.Scheme,
		Metrics:                server.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9c4404e7.operatorframework.io",
		Cache:                  cacheOptions,
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
		os.Exit(1)
	}

	coreClient, err := corev1client.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create core client")
		os.Exit(1)
	}
	tokenGetter := authentication.NewTokenGetter(coreClient, authentication.WithExpirationDuration(1*time.Hour))
	clientRestConfigMapper := action.ServiceAccountRestConfigMapper(tokenGetter)

	cfgGetter, err := helmclient.NewActionConfigGetter(mgr.GetConfig(), mgr.GetRESTMapper(),
		helmclient.StorageDriverMapper(action.ChunkedStorageDriverMapper(coreClient, mgr.GetAPIReader(), systemNamespace)),
		helmclient.ClientNamespaceMapper(func(obj client.Object) (string, error) {
			ext := obj.(*ocv1alpha1.ClusterExtension)
			return ext.Spec.Install.Namespace, nil
		}),
		helmclient.ClientRestConfigMapper(clientRestConfigMapper),
	)
	if err != nil {
		setupLog.Error(err, "unable to config for creating helm client")
		os.Exit(1)
	}

	acg, err := action.NewWrappedActionClientGetter(cfgGetter,
		helmclient.WithFailureRollbacks(false),
	)
	if err != nil {
		setupLog.Error(err, "unable to create helm client")
		os.Exit(1)
	}

	certPoolWatcher, err := httputil.NewCertPoolWatcher(caCertDir, ctrl.Log.WithName("cert-pool"))
	if err != nil {
		setupLog.Error(err, "unable to create CA certificate pool")
		os.Exit(1)
	}

	unpacker := &source.ContainersImageRegistry{
		BaseCachePath: filepath.Join(cachePath, "unpack"),
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
		}}

	clusterExtensionFinalizers := crfinalizer.NewFinalizers()
	if err := clusterExtensionFinalizers.Register(controllers.ClusterExtensionCleanupUnpackCacheFinalizer, finalizers.FinalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		return crfinalizer.Result{}, unpacker.Cleanup(ctx, &source.BundleSource{Name: obj.GetName()})
	})); err != nil {
		setupLog.Error(err, "unable to register finalizer", "finalizerKey", controllers.ClusterExtensionCleanupUnpackCacheFinalizer)
		os.Exit(1)
	}

	cl := mgr.GetClient()

	catalogsCachePath := filepath.Join(cachePath, "catalogs")
	if err := os.MkdirAll(catalogsCachePath, 0700); err != nil {
		setupLog.Error(err, "unable to create catalogs cache directory")
		os.Exit(1)
	}
	catalogClientBackend := cache.NewFilesystemCache(catalogsCachePath)
	catalogClient := catalogclient.New(catalogClientBackend, func() (*http.Client, error) {
		return httputil.BuildHTTPClient(certPoolWatcher)
	})

	resolver := &resolve.CatalogResolver{
		WalkCatalogsFunc: resolve.CatalogWalker(
			func(ctx context.Context, option ...client.ListOption) ([]catalogd.ClusterCatalog, error) {
				var catalogs catalogd.ClusterCatalogList
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
		os.Exit(1)
	}

	preflights := []applier.Preflight{
		crdupgradesafety.NewPreflight(aeClient.CustomResourceDefinitions()),
	}

	applier := &applier.Helm{
		ActionClientGetter: acg,
		Preflights:         preflights,
	}

	cm := contentmanager.NewManager(clientRestConfigMapper, mgr.GetConfig(), mgr.GetRESTMapper())
	err = clusterExtensionFinalizers.Register(controllers.ClusterExtensionCleanupContentManagerCacheFinalizer, finalizers.FinalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		ext := obj.(*ocv1alpha1.ClusterExtension)
		err := cm.Delete(ext)
		return crfinalizer.Result{}, err
	}))
	if err != nil {
		setupLog.Error(err, "unable to register content manager cleanup finalizer")
		os.Exit(1)
	}

	if err = (&controllers.ClusterExtensionReconciler{
		Client:                cl,
		Resolver:              resolver,
		Unpacker:              unpacker,
		Applier:               applier,
		InstalledBundleGetter: &controllers.DefaultInstalledBundleGetter{ActionClientGetter: acg},
		Finalizers:            clusterExtensionFinalizers,
		Manager:               cm,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterExtension")
		os.Exit(1)
	}

	if err = (&controllers.ClusterCatalogReconciler{
		Client:                cl,
		CatalogCache:          catalogClientBackend,
		CatalogCachePopulator: catalogClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterCatalog")
		os.Exit(1)
	}

	if globalPullSecretKey != nil {
		setupLog.Info("creating SecretSyncer controller for watching secret", "Secret", globalPullSecret)
		err := (&controllers.PullSecretReconciler{
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

	setupLog.Info("starting manager")
	ctx := ctrl.SetupSignalHandler()
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
	if err := os.Remove(authFilePath); err != nil {
		setupLog.Error(err, "failed to cleanup temporary auth file")
		os.Exit(1)
	}
}
