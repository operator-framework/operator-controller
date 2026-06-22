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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
	"github.com/operator-framework/operator-controller/internal/shared/util/image"
	mockcontrollers "github.com/operator-framework/operator-controller/internal/testutil/mock/controllers"
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

type warningCollector struct {
	mu    sync.Mutex
	items []string
}

func (w *warningCollector) HandleWarningHeader(code int, agent string, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.items = append(w.items, text)
}

func (w *warningCollector) hasWarning(substr string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, item := range w.items {
		if strings.Contains(item, substr) {
			return true
		}
	}
	return false
}

func newWarningCapturingClient(t *testing.T) (client.Client, *warningCollector) {
	collector := &warningCollector{}
	cfg := rest.CopyConfig(config)
	cfg.WarningHandler = collector
	cl, err := client.New(cfg, client.Options{Scheme: newScheme(t)})
	require.NoError(t, err)
	return cl, collector
}

// newMockRevisionStatesGetter creates a gomock-based RevisionStatesGetter
// that returns fixed values, replacing the hand-written MockRevisionStatesGetter.
func newMockRevisionStatesGetter(ctrl *gomock.Controller, revisionStates *controllers.RevisionStates, err error) *mockcontrollers.MockRevisionStatesGetter {
	m := mockcontrollers.NewMockRevisionStatesGetter(ctrl)
	m.EXPECT().GetRevisionStates(gomock.Any(), gomock.Any()).Return(revisionStates, err).AnyTimes()
	return m
}

// newMockApplier creates a gomock-based Applier that returns fixed values,
// replacing the hand-written MockApplier.
func newMockApplier(ctrl *gomock.Controller, installCompleted bool, err error) *mockcontrollers.MockApplier {
	m := mockcontrollers.NewMockApplier(ctrl)
	m.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(installCompleted, "", err).AnyTimes()
	return m
}

type reconcilerOption func(*deps)

type deps struct {
	RevisionStatesGetter controllers.RevisionStatesGetter
	Finalizers           crfinalizer.Finalizers
	Resolver             resolve.Resolver
	ImagePuller          image.Puller
	ImageCache           image.Cache
	Applier              controllers.Applier
	Validators           []controllers.ClusterExtensionValidator
}

func newClientAndReconciler(t *testing.T, opts ...reconcilerOption) (client.Client, *controllers.ClusterExtensionReconciler) {
	cl := newClient(t)

	mockCtrl := gomock.NewController(t)
	defaultRevisionStatesGetter := mockcontrollers.NewMockRevisionStatesGetter(mockCtrl)
	defaultRevisionStatesGetter.EXPECT().GetRevisionStates(gomock.Any(), gomock.Any()).Return(&controllers.RevisionStates{}, nil).AnyTimes()

	d := &deps{
		RevisionStatesGetter: defaultRevisionStatesGetter,
		Finalizers:           crfinalizer.NewFinalizers(),
	}
	reconciler := &controllers.ClusterExtensionReconciler{
		Client: cl,
	}
	for _, opt := range opts {
		opt(d)
	}
	reconciler.ReconcileSteps = []controllers.ReconcileStepFunc{
		controllers.HandleFinalizers(d.Finalizers),
		controllers.ValidateClusterExtension(d.Validators...),
		controllers.RetrieveRevisionStates(d.RevisionStatesGetter),
	}
	if r := d.Resolver; r != nil {
		reconciler.ReconcileSteps = append(reconciler.ReconcileSteps, controllers.ResolveBundle(r, cl))
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

	cl, err := client.New(config, client.Options{})
	utilruntime.Must(err)
	ctx := context.Background()
	utilruntime.Must(cl.Create(ctx, &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "clusterextension-serviceaccount-deprecated"},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
			MatchConstraints: &admissionregistrationv1.MatchResources{
				ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{{
					RuleWithOperations: admissionregistrationv1.RuleWithOperations{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"olm.operatorframework.io"},
							APIVersions: []string{"v1"},
							Resources:   []string{"clusterextensions"},
						},
					},
				}},
			},
			Validations: []admissionregistrationv1.Validation{{
				Expression: `!has(object.spec.serviceAccount) || !has(object.spec.serviceAccount.name) || object.spec.serviceAccount.name == ''`,
				Message:    "spec.serviceAccount is deprecated, ignored, and will be removed in a future release. The operator-controller's cluster-admin service account is used for all cluster interactions.",
			}},
		},
	}))
	utilruntime.Must(cl.Create(ctx, &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "clusterextension-serviceaccount-deprecated"},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName:        "clusterextension-serviceaccount-deprecated",
			ValidationActions: []admissionregistrationv1.ValidationAction{admissionregistrationv1.Warn},
		},
	}))

	code := m.Run()
	// Use Eventually wrapper for graceful test environment teardown
	// controller-runtime v0.23.0+ requires this to prevent timing-related errors
	stopErr := test.StopWithRetry(testEnv, time.Minute, time.Second)
	utilruntime.Must(stopErr)
	os.Exit(code)
}
