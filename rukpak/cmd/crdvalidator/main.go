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
	"os"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/operator-framework/rukpak/cmd/crdvalidator/handlers"
)

var (
	scheme   = runtime.NewScheme()
	entryLog = log.Log.WithName("crdvalidator")
)

const defaultCertDir = "/etc/admission-webhook/tls"

func init() {
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		entryLog.Error(err, "unable to set up crd scheme")
		os.Exit(1)
	}
}

func main() {
	// Setup a Manager
	entryLog.Info("setting up manager")
	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{Scheme: scheme})
	if err != nil {
		entryLog.Error(err, "unable to set up overall controller manager")
		os.Exit(1)
	}

	entryLog.Info("setting up webhook server")
	hookServer := mgr.GetWebhookServer()

	// Point to where cert-mgr is placing the cert
	hookServer.CertDir = defaultCertDir

	// Register CRD validation handler
	entryLog.Info("registering webhooks to the webhook server")
	crdValidatorHandler := handlers.NewCrdValidator(entryLog, mgr.GetClient())
	hookServer.Register("/validate-crd", &webhook.Admission{
		Handler: &crdValidatorHandler,
	})

	entryLog.Info("starting manager")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		entryLog.Error(err, "unable to run manager")
		os.Exit(1)
	}
}
