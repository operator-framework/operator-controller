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
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

func TestBoxcutterRevisionStatesGetter_ClassifiesByCompletedAt(t *testing.T) {
	sch := apimachineryruntime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(sch))

	const extName = "test-ext"

	newRevision := func(name string, revision int64, completed bool) *ocv1.ClusterObjectSet {
		cos := &ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{labels.OwnerNameKey: extName},
			},
			Spec: ocv1.ClusterObjectSetSpec{Revision: revision},
		}
		if completed {
			cos.Status.CompletedAt = metav1.Now()
		}
		return cos
	}

	completed := newRevision("test-ext-1", 1, true)
	rollingOut := newRevision("test-ext-2", 2, false)

	cl := fake.NewClientBuilder().
		WithScheme(sch).
		WithObjects(completed, rollingOut).
		Build()

	getter := &BoxcutterRevisionStatesGetter{Reader: cl}
	states, err := getter.GetRevisionStates(context.Background(), &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extName},
	})
	require.NoError(t, err)

	// A revision with completedAt set is Installed.
	require.NotNil(t, states.Installed)
	require.Equal(t, "test-ext-1", states.Installed.RevisionName)

	// A revision without completedAt is still RollingOut.
	require.Len(t, states.RollingOut, 1)
	require.Equal(t, "test-ext-2", states.RollingOut[0].RevisionName)
}
