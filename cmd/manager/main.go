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
	"crypto/x509"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/rukpak/pkg/finalizer"
	"github.com/operator-framework/rukpak/pkg/source"
	"github.com/operator-framework/rukpak/pkg/storage"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/cache"
	catalogclient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	"github.com/operator-framework/operator-controller/internal/controllers"
	"github.com/operator-framework/operator-controller/internal/handler"
	"github.com/operator-framework/operator-controller/internal/labels"
	"github.com/operator-framework/operator-controller/internal/version"
	"github.com/operator-framework/operator-controller/pkg/features"
	"github.com/operator-framework/operator-controller/pkg/scheme"
)

var (
	setupLog               = ctrl.Log.WithName("setup")
	defaultSystemNamespace = "operator-controller-system"
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
		metricsAddr                 string
		enableLeaderElection        bool
		probeAddr                   string
		cachePath                   string
		operatorControllerVersion   bool
		systemNamespace             string
		unpackCacheDir              string
		provisionerStorageDirectory string
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&cachePath, "cache-path", "/var/cache", "The local directory path used for filesystem based caching")
	flag.BoolVar(&operatorControllerVersion, "version", false, "Prints operator-controller version information")
	flag.StringVar(&systemNamespace, "system-namespace", "", "Configures the namespace that gets used to deploy system resources.")
	flag.StringVar(&unpackCacheDir, "unpack-cache-dir", "/var/cache/unpack", "Configures the directory that gets used to unpack and cache Bundle contents.")
	flag.StringVar(&provisionerStorageDirectory, "provisioner-storage-dir", storage.DefaultBundleCacheDir, "The directory that is used to store bundle contents.")
	opts := zap.Options{
		Development: true,
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

	dependentRequirement, err := k8slabels.NewRequirement(labels.OwnerKindKey, selection.In, []string{v1alpha1.ClusterExtensionKind})
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
				&v1alpha1.ClusterExtension{}: {},
			},
			DefaultNamespaces: map[string]crcache.Config{
				systemNamespace:       {},
				crcache.AllNamespaces: {LabelSelector: dependentSelector},
			},
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

	cl := mgr.GetClient()
	catalogClient := catalogclient.New(cl, cache.NewFilesystemCache(cachePath, &http.Client{Timeout: 10 * time.Second}))

	cfgGetter, err := helmclient.NewActionConfigGetter(mgr.GetConfig(), mgr.GetRESTMapper(), helmclient.StorageNamespaceMapper(func(o client.Object) (string, error) {
		return systemNamespace, nil
	}))
	if err != nil {
		setupLog.Error(err, "unable to config for creating helm client")
		os.Exit(1)
	}

	acg, err := helmclient.NewActionClientGetter(cfgGetter)
	if err != nil {
		setupLog.Error(err, "unable to create helm client")
		os.Exit(1)
	}

	bundleFinalizers := crfinalizer.NewFinalizers()
	unpacker, err := source.NewDefaultUnpacker(mgr, systemNamespace, unpackCacheDir, (*x509.CertPool)(nil))
	if err != nil {
		setupLog.Error(err, "unable to create unpacker")
		os.Exit(1)
	}

	if err := bundleFinalizers.Register(finalizer.CleanupUnpackCacheKey, &finalizer.CleanupUnpackCache{Unpacker: unpacker}); err != nil {
		setupLog.Error(err, "unable to register finalizer", "finalizerKey", finalizer.CleanupUnpackCacheKey)
		os.Exit(1)
	}

	localStorage := &storage.LocalDirectory{
		RootDirectory: provisionerStorageDirectory,
		URL:           url.URL{},
	}

	if err := bundleFinalizers.Register(finalizer.DeleteCachedBundleKey, &finalizer.DeleteCachedBundle{Storage: localStorage}); err != nil {
		setupLog.Error(err, "unable to register finalizer", "finalizerKey", finalizer.DeleteCachedBundleKey)
		os.Exit(1)
	}

	if err = (&controllers.ClusterExtensionReconciler{
		Client:                cl,
		ReleaseNamespace:      systemNamespace,
		BundleProvider:        catalogClient,
		ActionClientGetter:    acg,
		Unpacker:              unpacker,
		Storage:               localStorage,
		Handler:               handler.HandlerFunc(handler.HandleClusterExtension),
		InstalledBundleGetter: &controllers.DefaultInstalledBundleGetter{},
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
