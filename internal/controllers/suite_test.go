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
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/controllers"
	bd "github.com/operator-framework/operator-controller/internal/rukpak/bundledeployment"
	"github.com/operator-framework/operator-controller/internal/rukpak/source"
)

// MockUnpacker is a mock of Unpacker interface
type MockUnpacker struct {
	mock.Mock
}

// Unpack mocks the Unpack method
func (m *MockUnpacker) Unpack(ctx context.Context, bd *bd.BundleDeployment) (*source.Result, error) {
	args := m.Called(ctx, bd)
	return args.Get(0).(*source.Result), args.Error(1)
}

func (m *MockUnpacker) Cleanup(ctx context.Context, bundle *bd.BundleDeployment) error {
	//TODO implement me
	panic("implement me")
}

func newClient(t *testing.T) client.Client {
	// TODO: this is a live client, which behaves differently than a cache client.
	//  We may want to use a caching client instead to get closer to real behavior.
	sch := runtime.NewScheme()
	require.NoError(t, ocv1alpha1.AddToScheme(sch))
	cl, err := client.New(config, client.Options{Scheme: sch})
	require.NoError(t, err)
	require.NotNil(t, cl)
	return cl
}

type MockInstalledBundleGetter struct {
	bundle *ocv1alpha1.BundleMetadata
}

func (m *MockInstalledBundleGetter) SetBundle(bundle *ocv1alpha1.BundleMetadata) {
	m.bundle = bundle
}

func (m *MockInstalledBundleGetter) GetInstalledBundle(ctx context.Context, ext *ocv1alpha1.ClusterExtension) (*ocv1alpha1.BundleMetadata, error) {
	return m.bundle, nil
}

func newClientAndReconciler(t *testing.T, bundle *ocv1alpha1.BundleMetadata) (client.Client, *controllers.ClusterExtensionReconciler) {
	cl := newClient(t)

	reconciler := &controllers.ClusterExtensionReconciler{
		Client:                cl,
		ActionClientGetter:    helmClientGetter,
		Unpacker:              unpacker,
		InstalledBundleGetter: &MockInstalledBundleGetter{bundle},
		Finalizers:            crfinalizer.NewFinalizers(),
	}
	return cl, reconciler
}

var (
	config           *rest.Config
	helmClientGetter helmclient.ActionClientGetter
	unpacker         source.Unpacker // Interface, will be initialized as a mock in TestMain
)

func TestMain(m *testing.M) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "base", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	config, err = testEnv.Start()
	utilruntime.Must(err)
	if config == nil {
		log.Panic("expected cfg to not be nil")
	}

	rm := meta.NewDefaultRESTMapper(nil)
	cfgGetter, err := helmclient.NewActionConfigGetter(config, rm)
	utilruntime.Must(err)
	helmClientGetter, err = helmclient.NewActionClientGetter(cfgGetter)
	utilruntime.Must(err)

	unpacker = new(MockUnpacker)

	code := m.Run()
	utilruntime.Must(testEnv.Stop())
	os.Exit(code)
}
