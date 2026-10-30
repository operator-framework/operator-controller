/*
Copyright 2026.

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

package controllers

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/testutil"
)

func TestBoxcutterRevisionStatesGetter_ClassifiesAndFiltersRevisions(t *testing.T) {
	sch := apimachineryruntime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(sch))

	const extName = "test-ext"
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: extName, UID: "test-ext-uid"}}
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            extName,
			UID:             "test-cod-uid",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind))},
		},
	}

	newRevision := func(name string, revision int64, completed bool) *ocv1.ClusterObjectSet {
		cos := &ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cod, ocv1.GroupVersion.WithKind(ocv1.ClusterObjectDeploymentKind))},
			},
			Spec: ocv1.ClusterObjectSetSpec{Group: extName, Revision: revision},
		}
		if completed {
			cos.Status.CompletedAt = metav1.Now()
		}
		return cos
	}

	completed := newRevision("test-ext-1", 1, true)
	rollingOut := newRevision("test-ext-2", 2, false)
	archived := newRevision("test-ext-3", 3, true)
	archived.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
	foreign := newRevision("test-ext-4", 4, true)
	foreign.OwnerReferences[0].UID = "another-cod-uid"
	unowned := newRevision("test-ext-5", 5, true)
	unowned.SetOwnerReferences(nil)

	cl := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(sch), coscontrollers.SetupIndexes).
		WithObjects(cod, completed, rollingOut, archived, foreign, unowned).
		Build()

	getter := &BoxcutterRevisionStatesGetter{Reader: cl}
	states, err := getter.GetRevisionStates(context.Background(), ext)
	require.NoError(t, err)

	// A revision with completedAt set is Installed.
	require.NotNil(t, states.Installed)
	require.Equal(t, "test-ext-1", states.Installed.RevisionName)

	// A revision without completedAt is still RollingOut.
	require.Len(t, states.RollingOut, 1)
	require.Equal(t, "test-ext-2", states.RollingOut[0].RevisionName)

	// The foreign, archived, and unowned revisions should not be reported at all.
	require.NotContains(t, allRevisionNames(states), foreign.Name)
	require.NotContains(t, allRevisionNames(states), archived.Name)
	require.NotContains(t, allRevisionNames(states), unowned.Name)
}

func allRevisionNames(states *RevisionStates) []string {
	if states == nil {
		return nil
	}
	names := sets.Set[string]{}
	if states.Installed != nil {
		names.Insert(states.Installed.RevisionName)
	}
	for _, rollingOut := range states.RollingOut {
		if rollingOut != nil {
			names.Insert(rollingOut.RevisionName)
		}
	}
	return sets.List[string](names)
}

func TestBoxcutterRevisionStatesGetter_DoesNotReportUnownedResources(t *testing.T) {
	sch := apimachineryruntime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(sch))

	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext", UID: "test-ext-uid"}}
	unownedCOS := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-1", ext.Name),
		},
		Spec:   ocv1.ClusterObjectSetSpec{Group: ext.Name, Revision: 1},
		Status: ocv1.ClusterObjectSetStatus{CompletedAt: metav1.Now()},
	}
	otherCOD := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: ext.Name,
			UID:  "test-cod-uid",
		},
	}
	otherCOS := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-1", ext.Name),
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(otherCOD, ocv1.GroupVersion.WithKind(ocv1.ClusterObjectDeploymentKind))},
		},
		Spec:   ocv1.ClusterObjectSetSpec{Group: ext.Name, Revision: 1},
		Status: ocv1.ClusterObjectSetStatus{CompletedAt: metav1.Now()},
	}

	for _, tc := range []struct {
		name    string
		objects []client.Object
	}{
		{name: "missing deployment", objects: []client.Object{unownedCOS}},
		{name: "COD/COS not owned by extension", objects: []client.Object{otherCOD, otherCOS}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(tc.objects...).Build()
			getter := &BoxcutterRevisionStatesGetter{Reader: cl}

			states, err := getter.GetRevisionStates(context.Background(), ext)
			require.NoError(t, err)
			require.Equal(t, &RevisionStates{}, states)
		})
	}
}
