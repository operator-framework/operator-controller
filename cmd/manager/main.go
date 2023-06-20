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
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/spf13/pflag"

	"github.com/operator-framework/catalogd/internal/source"
	"github.com/operator-framework/catalogd/internal/version"
	corecontrollers "github.com/operator-framework/catalogd/pkg/controllers/core"
	"github.com/operator-framework/catalogd/pkg/features"
	"github.com/operator-framework/catalogd/pkg/profile"

	//+kubebuilder:scaffold:imports
	"github.com/operator-framework/catalogd/api/core/v1alpha1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		unpackImage          string
		profiling            bool
		catalogdVersion      bool
		sysNs                string
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	// TODO: should we move the unpacker to some common place? Or... hear me out... should catalogd just be a rukpak provisioner?
	flag.StringVar(&unpackImage, "unpack-image", "quay.io/operator-framework/rukpak:v0.12.0", "The unpack image to use when unpacking catalog images")
	flag.StringVar(&sysNs, "system-ns", "catalogd-system", "The namespace catalogd uses for internal state, configuration, and workloads")
	flag.BoolVar(&profiling, "profiling", false, "enable profiling endpoints to allow for using pprof")
	flag.BoolVar(&catalogdVersion, "version", false, "print the catalogd version and exit")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	// Combine both flagsets and parse them
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	features.CatalogdFeatureGate.AddFlag(pflag.CommandLine)
	pflag.Parse()

	if catalogdVersion {
		fmt.Printf("%#v\n", version.Version())
		os.Exit(0)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "catalogd-operator-lock",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	unpacker, err := source.NewDefaultUnpacker(mgr, sysNs, unpackImage)
	if err != nil {
		setupLog.Error(err, "unable to create unpacker")
		os.Exit(1)
	}

	if err = (&corecontrollers.CatalogReconciler{
		Client:   mgr.GetClient(),
		Unpacker: unpacker,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Catalog")
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

	if profiling {
		pprofer := profile.NewPprofer()
		if err := pprofer.ConfigureControllerManager(mgr); err != nil {
			setupLog.Error(err, "unable to setup pprof configuration")
			os.Exit(1)
		}
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
