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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/contentmanager"
	cmcache "github.com/operator-framework/operator-controller/internal/contentmanager/cache"
	"github.com/operator-framework/operator-controller/internal/controllers"
	"github.com/operator-framework/operator-controller/internal/rukpak/source"
)

// MockUnpacker is a mock of Unpacker interface
type MockUnpacker struct {
	err    error
	result *source.Result
}

// Unpack mocks the Unpack method
func (m *MockUnpacker) Unpack(_ context.Context, _ *source.BundleSource) (*source.Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func (m *MockUnpacker) Cleanup(_ context.Context, _ *source.BundleSource) error {
	// TODO implement me
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
	bundle *controllers.InstalledBundle
}

func (m *MockInstalledBundleGetter) SetBundle(bundle *controllers.InstalledBundle) {
	m.bundle = bundle
}

func (m *MockInstalledBundleGetter) GetInstalledBundle(ctx context.Context, ext *ocv1alpha1.ClusterExtension) (*controllers.InstalledBundle, error) {
	return m.bundle, nil
}

var _ controllers.Applier = (*MockApplier)(nil)

type MockApplier struct {
	err   error
	objs  []client.Object
	state string
}

func (m *MockApplier) Apply(_ context.Context, _ fs.FS, _ *ocv1alpha1.ClusterExtension, _ map[string]string, _ map[string]string) ([]client.Object, string, error) {
	if m.err != nil {
		return nil, m.state, m.err
	}

	return m.objs, m.state, nil
}

var _ contentmanager.Manager = (*MockManagedContentCacheManager)(nil)

type MockManagedContentCacheManager struct {
	err   error
	cache cmcache.Cache
}

func (m *MockManagedContentCacheManager) Get(_ context.Context, _ *ocv1alpha1.ClusterExtension) (cmcache.Cache, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.cache, nil
}

func (m *MockManagedContentCacheManager) Delete(_ *ocv1alpha1.ClusterExtension) error {
	return m.err
}

type MockManagedContentCache struct {
	err error
}

var _ cmcache.Cache = (*MockManagedContentCache)(nil)

func (m *MockManagedContentCache) Close() error {
	if m.err != nil {
		return m.err
	}
	return nil
}

func (m *MockManagedContentCache) Watch(_ context.Context, _ cmcache.Watcher, _ ...client.Object) error {
	if m.err != nil {
		return m.err
	}
	return nil
}

func newClientAndReconciler(t *testing.T) (client.Client, *controllers.ClusterExtensionReconciler) {
	cl := newClient(t)

	reconciler := &controllers.ClusterExtensionReconciler{
		Client:                cl,
		InstalledBundleGetter: &MockInstalledBundleGetter{},
		Finalizers:            crfinalizer.NewFinalizers(),
	}
	return cl, reconciler
}

var (
	config           *rest.Config
	helmClientGetter helmclient.ActionClientGetter
)

func TestMain(m *testing.M) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "base", "crd", "bases"),
		},
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

	code := m.Run()
	utilruntime.Must(testEnv.Stop())
	os.Exit(code)
}
