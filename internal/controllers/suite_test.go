/*
Copyright 2023.

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

package controllers_test

import (
	"crypto/x509"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/operator-framework/deppy/pkg/deppy/solver"
	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/rukpak/pkg/source"
	"github.com/operator-framework/rukpak/pkg/util"

	"github.com/operator-framework/operator-controller/internal/controllers"
	"github.com/operator-framework/operator-controller/pkg/scheme"
	testutil "github.com/operator-framework/operator-controller/test/util"
)

func newClient(t *testing.T) client.Client {
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, cl)
	return cl
}

func newClientAndReconciler(t *testing.T) (client.Client, *controllers.ClusterExtensionReconciler) {
	_, err := solver.New()
	require.NoError(t, err)

	cl := newClient(t)
	fakeCatalogClient := testutil.NewFakeCatalogClient(testBundleList)
	reconciler := &controllers.ClusterExtensionReconciler{
		Client:             cl,
		BundleProvider:     &fakeCatalogClient,
		Scheme:             scheme.Scheme,
		ActionClientGetter: acg,
		Unpacker:           unp,
	}
	return cl, reconciler
}

func newClientAndExtensionReconciler(t *testing.T) (client.Client, *controllers.ExtensionReconciler) {
	cl := newClient(t)
	fakeCatalogClient := testutil.NewFakeCatalogClient(testBundleList)
	reconciler := &controllers.ExtensionReconciler{
		Client:         cl,
		BundleProvider: &fakeCatalogClient,
	}
	return cl, reconciler
}

var (
	cfg *rest.Config
	acg helmclient.ActionClientGetter
	unp source.Unpacker
)

func TestMain(m *testing.M) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "testdata", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	utilruntime.Must(err)
	if cfg == nil {
		log.Panic("expected cfg to not be nil")
	}

	rm := meta.NewDefaultRESTMapper(nil)
	cfgGetter, err := helmclient.NewActionConfigGetter(cfg, rm)
	utilruntime.Must(err)
	acg, err = helmclient.NewActionClientGetter(cfgGetter)
	utilruntime.Must(err)

	mgr, err := manager.New(cfg, manager.Options{})
	utilruntime.Must(err)
	unp, err = source.NewDefaultUnpacker(mgr, util.DefaultSystemNamespace, util.DefaultUnpackImage, (*x509.CertPool)(nil))
	utilruntime.Must(err)

	code := m.Run()
	utilruntime.Must(testEnv.Stop())
	os.Exit(code)
}
