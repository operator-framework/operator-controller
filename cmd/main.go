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
	"flag"
	"os"

	"k8s.io/client-go/discovery"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	configv1 "github.com/openshift/api/config/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	platformv1alpha1 "github.com/openshift/api/platform/v1alpha1"
	"github.com/timflannagan/platform-operators/internal/clusteroperator"
	"github.com/timflannagan/platform-operators/internal/controllers"
	"github.com/timflannagan/platform-operators/internal/sourcer"
	"github.com/timflannagan/platform-operators/internal/util"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(operatorsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(rukpakv1alpha1.AddToScheme(scheme))
	utilruntime.Must(platformv1alpha1.Install(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		systemNamespace      string
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&systemNamespace, "system-namespace", "openshift-platform-operators", "Configures the namespace that gets used to deploy system resources.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "ffdf93bc.openshift.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.PlatformOperatorReconciler{
		Client:  mgr.GetClient(),
		Sourcer: sourcer.NewCatalogSourceHandler(mgr.GetClient()),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PlatformOperator")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	// check whether the ClusterOperator GV exists on the cluster to determine whether
	// the aggregate ClusterOperator controller should be setup.
	if err := registerCOControllersIfAvailable(mgr, systemNamespace); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterOperator")
		os.Exit(1)
	}

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

// registerCOControllersIfAvailable is responsible for checking whether
// the config.openshift.io/v1 GV is available on the cluster to determine
// whether the ClusterOperator-related controllers should be added to the
// mgr instance.
func registerCOControllersIfAvailable(mgr ctrl.Manager, systemNamespace string) error {
	discovery, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}
	supported, err := util.IsAPIAvailable(discovery, schema.GroupVersion{
		Group:   "config.openshift.io",
		Version: "v1",
	})
	if err != nil {
		return err
	}
	if !supported {
		return nil
	}
	// Add Aggregated CO controller to manager
	return (&controllers.AggregatedClusterOperatorReconciler{
		Client:          mgr.GetClient(),
		ReleaseVersion:  clusteroperator.GetReleaseVariable(),
		SystemNamespace: util.PodNamespace(systemNamespace),
	}).SetupWithManager(mgr)
}
