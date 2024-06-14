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
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/rukpak/api/v1alpha2"
	"github.com/operator-framework/rukpak/pkg/source"
	"github.com/operator-framework/rukpak/pkg/storage"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/controllers"
	"github.com/operator-framework/operator-controller/pkg/scheme"
	testutil "github.com/operator-framework/operator-controller/test/util"
)

// MockUnpacker is a mock of Unpacker interface
type MockUnpacker struct {
	mock.Mock
}

// Unpack mocks the Unpack method
func (m *MockUnpacker) Unpack(ctx context.Context, bd *v1alpha2.BundleDeployment) (*source.Result, error) {
	args := m.Called(ctx, bd)
	return args.Get(0).(*source.Result), args.Error(1)
}

func (m *MockUnpacker) Cleanup(ctx context.Context, bundle *v1alpha2.BundleDeployment) error {
	//TODO implement me
	panic("implement me")
}

// MockStorage is a mock of Storage interface
type MockStorage struct {
	mock.Mock
}

func (m *MockStorage) Load(ctx context.Context, owner client.Object) (fs.FS, error) {
	args := m.Called(ctx, owner)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(fs.FS), args.Error(1)
}

func (m *MockStorage) Delete(ctx context.Context, owner client.Object) error {
	//TODO implement me
	panic("implement me")
}

func (m *MockStorage) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	//TODO implement me
	panic("implement me")
}

func (m *MockStorage) URLFor(ctx context.Context, owner client.Object) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockStorage) Store(ctx context.Context, owner client.Object, bundle fs.FS) error {
	args := m.Called(ctx, owner, bundle)
	return args.Error(0)
}

func newClient(t *testing.T) client.Client {
	cl, err := client.New(config, client.Options{Scheme: scheme.Scheme})
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
	fakeCatalogClient := testutil.NewFakeCatalogClient(testBundleList)

	mockInstalledBundleGetter := &MockInstalledBundleGetter{}
	mockInstalledBundleGetter.SetBundle(bundle)

	reconciler := &controllers.ClusterExtensionReconciler{
		Client:                cl,
		BundleProvider:        &fakeCatalogClient,
		ActionClientGetter:    helmClientGetter,
		Unpacker:              unpacker,
		Storage:               store,
		InstalledBundleGetter: mockInstalledBundleGetter,
		Finalizers:            crfinalizer.NewFinalizers(),
	}
	return cl, reconciler
}

var (
	config           *rest.Config
	helmClientGetter helmclient.ActionClientGetter
	unpacker         source.Unpacker // Interface, will be initialized as a mock in TestMain
	store            storage.Storage
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
	store = new(MockStorage)

	code := m.Run()
	utilruntime.Must(testEnv.Stop())
	os.Exit(code)
}
