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
	"time"

	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/action"
	"github.com/operator-framework/operator-controller/internal/authentication"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/cache"
	catalogclient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	"github.com/operator-framework/operator-controller/internal/controllers"
	"github.com/operator-framework/operator-controller/internal/httputil"
	"github.com/operator-framework/operator-controller/internal/labels"
	"github.com/operator-framework/operator-controller/internal/resolve"
	"github.com/operator-framework/operator-controller/internal/rukpak/preflights/crdupgradesafety"
	"github.com/operator-framework/operator-controller/internal/rukpak/source"
	"github.com/operator-framework/operator-controller/internal/version"
	"github.com/operator-framework/operator-controller/pkg/features"
	"github.com/operator-framework/operator-controller/pkg/scheme"
)

var (
	setupLog               = ctrl.Log.WithName("setup")
	defaultSystemNamespace = "olmv1-system"
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

func main() {
	var (
		metricsAddr               string
		enableLeaderElection      bool
		probeAddr                 string
		cachePath                 string
		operatorControllerVersion bool
		systemNamespace           string
		caCertDir                 string
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
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	features.OperatorControllerFeatureGate.AddFlag(pflag.CommandLine)
	pflag.Parse()

	if operatorControllerVersion {
		fmt.Println(version.String())
		os.Exit(0)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts), zap.StacktraceLevel(zapcore.DPanicLevel)))
	setupLog.Info("starting up the controller", "version info", version.String())

	if systemNamespace == "" {
		systemNamespace = podNamespace()
	}

	dependentRequirement, err := k8slabels.NewRequirement(labels.OwnerKindKey, selection.In, []string{ocv1alpha1.ClusterExtensionKind})
	if err != nil {
		setupLog.Error(err, "unable to create dependent label selector for cache")
		os.Exit(1)
	}
	dependentSelector := k8slabels.NewSelector().Add(*dependentRequirement)

	setupLog.Info("set up manager")
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme.Scheme,
		Metrics:                server.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9c4404e7.operatorframework.io",
		Cache: crcache.Options{
			ByObject: map[client.Object]crcache.ByObject{
				&ocv1alpha1.ClusterExtension{}: {Label: k8slabels.Everything()},
				&catalogd.ClusterCatalog{}:     {Label: k8slabels.Everything()},
			},
			DefaultNamespaces: map[string]crcache.Config{
				systemNamespace: {LabelSelector: k8slabels.Everything()},
			},
			DefaultLabelSelector: dependentSelector,
		},
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

	installNamespaceMapper := helmclient.ObjectToStringMapper(func(obj client.Object) (string, error) {
		ext := obj.(*ocv1alpha1.ClusterExtension)
		return ext.Spec.InstallNamespace, nil
	})
	coreClient, err := corev1client.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create core client")
		os.Exit(1)
	}
	tokenGetter := authentication.NewTokenGetter(coreClient, authentication.WithExpirationDuration(1*time.Hour))

	restConfigMapper := func(ctx context.Context, o client.Object, c *rest.Config) (*rest.Config, error) {
		cExt, ok := o.(*ocv1alpha1.ClusterExtension)
		if !ok {
			return c, nil
		}
		namespacedName := types.NamespacedName{
			Name:      cExt.Spec.ServiceAccount.Name,
			Namespace: cExt.Spec.InstallNamespace,
		}
		token, err := tokenGetter.Get(ctx, namespacedName)
		if err != nil {
			return nil, fmt.Errorf("failed to extract SA token, %w", err)
		}
		tempConfig := rest.AnonymousClientConfig(c)
		tempConfig.BearerToken = token
		return tempConfig, nil
	}
	cfgGetter, err := helmclient.NewActionConfigGetter(mgr.GetConfig(), mgr.GetRESTMapper(),
		helmclient.StorageNamespaceMapper(installNamespaceMapper),
		helmclient.ClientNamespaceMapper(installNamespaceMapper),
		helmclient.RestConfigMapper(restConfigMapper),
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
	unpacker := &source.ImageRegistry{
		BaseCachePath: filepath.Join(cachePath, "unpack"),
		// TODO: This needs to be derived per extension via ext.Spec.InstallNamespace
		AuthNamespace:   systemNamespace,
		CertPoolWatcher: certPoolWatcher,
	}

	clusterExtensionFinalizers := crfinalizer.NewFinalizers()
	domain := ocv1alpha1.GroupVersion.Group
	cleanupUnpackCacheKey := fmt.Sprintf("%s/cleanup-unpack-cache", domain)
	if err := clusterExtensionFinalizers.Register(cleanupUnpackCacheKey, finalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		ext := obj.(*ocv1alpha1.ClusterExtension)
		return crfinalizer.Result{}, os.RemoveAll(filepath.Join(unpacker.BaseCachePath, ext.GetName()))
	})); err != nil {
		setupLog.Error(err, "unable to register finalizer", "finalizerKey", cleanupUnpackCacheKey)
		os.Exit(1)
	}

	aeClient, err := apiextensionsv1client.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create apiextensions client")
		os.Exit(1)
	}

	preflights := []controllers.Preflight{
		crdupgradesafety.NewPreflight(aeClient.CustomResourceDefinitions()),
	}

	cl := mgr.GetClient()

	catalogsCachePath := filepath.Join(cachePath, "catalogs")
	if err := os.MkdirAll(catalogsCachePath, 0700); err != nil {
		setupLog.Error(err, "unable to create catalogs cache directory")
		os.Exit(1)
	}
	catalogClient := catalogclient.New(cache.NewFilesystemCache(catalogsCachePath, func() (*http.Client, error) {
		return httputil.BuildHTTPClient(certPoolWatcher)
	}))

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
	}

	if err = (&controllers.ClusterExtensionReconciler{
		Client:                cl,
		Resolver:              resolver,
		ActionClientGetter:    acg,
		Unpacker:              unpacker,
		InstalledBundleGetter: &controllers.DefaultInstalledBundleGetter{ActionClientGetter: acg},
		Finalizers:            clusterExtensionFinalizers,
		Preflights:            preflights,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterExtension")
		os.Exit(1)
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
}

type finalizerFunc func(ctx context.Context, obj client.Object) (crfinalizer.Result, error)

func (f finalizerFunc) Finalize(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
	return f(ctx, obj)
}
