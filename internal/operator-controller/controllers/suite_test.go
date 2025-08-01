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
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager"
	cmcache "github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager/cache"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
)

func newScheme(t *testing.T) *apimachineryruntime.Scheme {
	sch := apimachineryruntime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(sch))
	return sch
}

func newClient(t *testing.T) client.Client {
	// TODO: this is a live client, which behaves differently than a cache client.
	//  We may want to use a caching client instead to get closer to real behavior.
	cl, err := client.New(config, client.Options{Scheme: newScheme(t)})
	require.NoError(t, err)
	require.NotNil(t, cl)
	return cl
}

type MockInstalledBundleGetter struct {
	bundle *controllers.RevisionMetadata
	err    error
}

func (m *MockInstalledBundleGetter) SetBundle(bundle *controllers.RevisionMetadata) {
	m.bundle = bundle
}

func (m *MockInstalledBundleGetter) GetInstalledBundle(ctx context.Context, ext *ocv1.ClusterExtension) (*controllers.RevisionMetadata, error) {
	return m.bundle, m.err
}

var _ controllers.Applier = (*MockApplier)(nil)

type MockApplier struct {
	err   error
	objs  []client.Object
	state string
}

func (m *MockApplier) Apply(_ context.Context, _ fs.FS, _ *ocv1.ClusterExtension, _ map[string]string, _ map[string]string) ([]client.Object, string, error) {
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

func (m *MockManagedContentCacheManager) Get(_ context.Context, _ *ocv1.ClusterExtension) (cmcache.Cache, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.cache, nil
}

func (m *MockManagedContentCacheManager) Delete(_ *ocv1.ClusterExtension) error {
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
			filepath.Join("..", "..", "..", "config", "base", "operator-controller", "crd", "experimental"),
		},
		ErrorIfCRDPathMissing: true,
	}

	// ENVTEST-based tests require specific binaries. By default, these binaries are located
	// in paths defined by controller-runtime. However, the `BinaryAssetsDirectory` needs
	// to be explicitly set when running tests directly (e.g., debugging tests in an IDE)
	// without using the Makefile targets.
	//
	// This is equivalent to configuring your IDE to export the `KUBEBUILDER_ASSETS` environment
	// variable before each test execution. The following function simplifies this process
	// by handling the configuration for you.
	//
	// To ensure the binaries are in the expected path without manual configuration, run:
	// `make envtest-k8s-bins`
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
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

// getFirstFoundEnvTestBinaryDir finds and returns the first directory under the given path.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "envtest-binaries", "k8s")
	entries, _ := os.ReadDir(basePath)
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
