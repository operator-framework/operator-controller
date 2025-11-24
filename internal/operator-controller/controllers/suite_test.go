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
	"testing"

	"github.com/stretchr/testify/require"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
	"github.com/operator-framework/operator-controller/internal/shared/util/image"
	"github.com/operator-framework/operator-controller/test"
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

var _ controllers.RevisionStatesGetter = (*MockRevisionStatesGetter)(nil)

type MockRevisionStatesGetter struct {
	*controllers.RevisionStates
	Err error
}

func (m *MockRevisionStatesGetter) GetRevisionStates(ctx context.Context, ext *ocv1.ClusterExtension) (*controllers.RevisionStates, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.RevisionStates, nil
}

var _ controllers.Applier = (*MockApplier)(nil)

type MockApplier struct {
	installCompleted bool
	installStatus    string
	err              error
}

func (m *MockApplier) Apply(_ context.Context, _ fs.FS, _ *ocv1.ClusterExtension, _ map[string]string, _ map[string]string) (bool, string, error) {
	return m.installCompleted, m.installStatus, m.err
}

type reconcilerOption func(*deps)

type deps struct {
	RevisionStatesGetter controllers.RevisionStatesGetter
	Finalizers           crfinalizer.Finalizers
	Resolver             resolve.Resolver
	ImagePuller          image.Puller
	ImageCache           image.Cache
	Applier              controllers.Applier
}

func newClientAndReconciler(t *testing.T, opts ...reconcilerOption) (client.Client, *controllers.ClusterExtensionReconciler) {
	cl := newClient(t)

	d := &deps{
		RevisionStatesGetter: &MockRevisionStatesGetter{
			RevisionStates: &controllers.RevisionStates{},
		},
		Finalizers: crfinalizer.NewFinalizers(),
	}
	reconciler := &controllers.ClusterExtensionReconciler{
		Client: cl,
	}
	for _, opt := range opts {
		opt(d)
	}
	reconciler.ReconcileSteps = []controllers.ReconcileStepFunc{controllers.HandleFinalizers(d.Finalizers), controllers.RetrieveRevisionStates(d.RevisionStatesGetter)}
	if r := d.Resolver; r != nil {
		reconciler.ReconcileSteps = append(reconciler.ReconcileSteps, controllers.ResolveBundle(r))
	}
	if i := d.ImagePuller; i != nil {
		reconciler.ReconcileSteps = append(reconciler.ReconcileSteps, controllers.UnpackBundle(i, d.ImageCache))
	}
	if a := d.Applier; a != nil {
		reconciler.ReconcileSteps = append(reconciler.ReconcileSteps, controllers.ApplyBundle(a))
	}

	return cl, reconciler
}

var config *rest.Config

func TestMain(m *testing.M) {
	testEnv := test.NewEnv()

	var err error
	config, err = testEnv.Start()
	utilruntime.Must(err)
	if config == nil {
		log.Panic("expected cfg to not be nil")
	}

	code := m.Run()
	utilruntime.Must(testEnv.Stop())
	os.Exit(code)
}
