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
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/rukpak/pkg/source"
	"github.com/operator-framework/rukpak/pkg/storage"
	"github.com/operator-framework/rukpak/pkg/util"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/cache"
	catalogclient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	"github.com/operator-framework/operator-controller/internal/controllers"
	"github.com/operator-framework/operator-controller/internal/handler"
	"github.com/operator-framework/operator-controller/internal/labels"
	"github.com/operator-framework/operator-controller/pkg/features"
	"github.com/operator-framework/operator-controller/pkg/scheme"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

func main() {
	var (
		metricsAddr                 string
		enableLeaderElection        bool
		probeAddr                   string
		cachePath                   string
		httpExternalAddr            string
		systemNamespace             string
		unpackImage                 string
		provisionerStorageDirectory string
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&httpExternalAddr, "http-external-address", "http://localhost:8080", "The external address at which the http server is reachable.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&cachePath, "cache-path", "/var/cache", "The local directory path used for filesystem based caching")
	flag.StringVar(&systemNamespace, "system-namespace", "", "Configures the namespace that gets used to deploy system resources.")
	flag.StringVar(&unpackImage, "unpack-image", util.DefaultUnpackImage, "Configures the container image that gets used to unpack Bundle contents.")
	flag.StringVar(&provisionerStorageDirectory, "provisioner-storage-dir", storage.DefaultBundleCacheDir, "The directory that is used to store bundle contents.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	features.OperatorControllerFeatureGate.AddFlag(pflag.CommandLine)
	pflag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts), zap.StacktraceLevel(zapcore.DPanicLevel)))

	if systemNamespace == "" {
		systemNamespace = util.PodNamespace()
	}

	dependentRequirement, err := k8slabels.NewRequirement(labels.OwnerKindKey, selection.In, []string{v1alpha1.ClusterExtensionKind})
	if err != nil {
		setupLog.Error(err, "unable to create dependent label selector for cache")
		os.Exit(1)
	}
	dependentSelector := k8slabels.NewSelector().Add(*dependentRequirement)

	fmt.Println("set up manager")
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

	unpacker, err := source.NewDefaultUnpacker(mgr, systemNamespace, unpackImage, (*x509.CertPool)(nil))
	if err != nil {
		setupLog.Error(err, "unable to create unpacker")
		os.Exit(1)
	}

	storageURL, err := url.Parse(fmt.Sprintf("%s/bundles/", httpExternalAddr))
	if err != nil {
		setupLog.Error(err, "unable to parse bundle content server URL")
		os.Exit(1)
	}

	localStorage := &storage.LocalDirectory{
		RootDirectory: provisionerStorageDirectory,
		URL:           *storageURL,
	}

	if err = (&controllers.ClusterExtensionReconciler{
		Client:             cl,
		ReleaseNamespace:   systemNamespace,
		BundleProvider:     catalogClient,
		Scheme:             mgr.GetScheme(),
		ActionClientGetter: acg,
		Unpacker:           unpacker,
		Storage:            localStorage,
		Handler:            handler.HandlerFunc(handler.HandleClusterExtension),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterExtension")
		os.Exit(1)
	}

	// if err = (&controllers.ExtensionReconciler{
	// 	Client:         cl,
	// 	BundleProvider: catalogClient,
	// }).SetupWithManager(mgr); err != nil {
	// 	setupLog.Error(err, "unable to create controller", "controller", "Extension")
	// 	os.Exit(1)
	// }
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
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
