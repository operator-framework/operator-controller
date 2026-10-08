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

package revisions_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/revisions"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	"github.com/operator-framework/operator-controller/test"
)

func TestListForClusterExtension(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext", UID: "current-uid"}}
	owner := *metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind))
	differentName, differentKind, differentIdentity := owner, owner, owner
	differentName.Name = "other-ext"
	differentKind.Kind = "OtherController"
	differentIdentity.UID = "previous-uid"
	differentIdentity.APIVersion = "olm.operatorframework.io/v2"
	nonController, unspecifiedController := owner, owner
	nonController.Controller = ptr.To(false)
	unspecifiedController.Controller = nil

	objects := make([]client.Object, 0, 9)
	for _, tc := range []struct {
		name     string
		group    string
		owners   []metav1.OwnerReference
		revision int64
	}{
		{name: "latest", group: ext.Name, owners: []metav1.OwnerReference{owner}, revision: 2},
		{name: "oldest", group: ext.Name, owners: []metav1.OwnerReference{differentIdentity}, revision: 1},
		{name: "other-group", group: "other-group", owners: []metav1.OwnerReference{owner}, revision: 99},
		{name: "other-name", group: ext.Name, owners: []metav1.OwnerReference{differentName}, revision: 100},
		{name: "other-kind", group: ext.Name, owners: []metav1.OwnerReference{differentKind}, revision: 101},
		{name: "ownerless", group: ext.Name, revision: 102},
		{name: "non-controller", group: ext.Name, owners: []metav1.OwnerReference{nonController}, revision: 103},
		{name: "unspecified-controller", group: ext.Name, owners: []metav1.OwnerReference{unspecifiedController}, revision: 104},
		{name: "foreign-controller-with-ce-owner", group: ext.Name, owners: []metav1.OwnerReference{differentKind, nonController}, revision: 105},
	} {
		objects = append(objects, &ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{Name: tc.name, OwnerReferences: tc.owners},
			Spec:       ocv1.ClusterObjectSetSpec{Group: tc.group, Revision: tc.revision},
		})
	}
	// Labels cannot cause foreign revisions to be included or owned ones to be excluded.
	for _, object := range objects[2:] {
		object.SetLabels(map[string]string{labels.OwnerNameKey: ext.Name})
	}
	cl := test.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithObjects(objects...).Build()
	got, err := revisions.ListForClusterExtension(t.Context(), cl, ext)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "oldest", got[0].Name)
	require.Equal(t, "latest", got[1].Name)
}
