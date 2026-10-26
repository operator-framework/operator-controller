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
	"testing"

	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

// codWithProgressing builds a ClusterObjectDeployment whose Progressing condition has the given
// status/reason, observed at the deployment's generation.
func codWithProgressing(name string, generation int64, status metav1.ConditionStatus, reason, message string) *ocv1.ClusterObjectDeployment {
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Generation: generation},
	}
	apimeta.SetStatusCondition(&cod.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterObjectDeploymentTypeProgressing,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
	return cod
}

func TestProgressingConditionFromClusterObjectDeployment(t *testing.T) {
	for _, tc := range []struct {
		name string
		cod  *metav1.Condition
		want metav1.Condition
	}{
		{
			name: "nil deployment condition defaults to RollingOut",
			cod:  nil,
			want: metav1.Condition{Status: metav1.ConditionTrue, Reason: ocv1.ReasonRollingOut, Message: "Revision is rolling out."},
		},
		{
			name: "progressing maps to RollingOut",
			cod:  &metav1.Condition{Status: metav1.ConditionTrue, Reason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, Message: "latest revision is progressing"},
			want: metav1.Condition{Status: metav1.ConditionTrue, Reason: ocv1.ReasonRollingOut, Message: "latest revision is progressing"},
		},
		{
			name: "progressed (deployment False) maps to Succeeded (extension True)",
			cod:  &metav1.Condition{Status: metav1.ConditionFalse, Reason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, Message: "latest revision has progressed"},
			want: metav1.Condition{Status: metav1.ConditionTrue, Reason: ocv1.ReasonSucceeded, Message: "Desired state reached"},
		},
		{
			name: "object collision (deployment False) maps to Retrying (extension True)",
			cod:  &metav1.Condition{Status: metav1.ConditionFalse, Reason: ocv1.ClusterObjectDeploymentReasonRevisionObjectCollision, Message: "owned by another controller"},
			want: metav1.Condition{Status: metav1.ConditionTrue, Reason: ocv1.ReasonRetrying, Message: "owned by another controller"},
		},
		{
			name: "preconditions not met maps to Retrying",
			cod:  &metav1.Condition{Status: metav1.ConditionTrue, Reason: ocv1.ClusterObjectDeploymentReasonRevisionPreconditionsNotMet, Message: "waiting on secret"},
			want: metav1.Condition{Status: metav1.ConditionTrue, Reason: ocv1.ReasonRetrying, Message: "waiting on secret"},
		},
		{
			name: "progress deadline exceeded passes through",
			cod:  &metav1.Condition{Status: metav1.ConditionFalse, Reason: ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, Message: "deadline exceeded"},
			want: metav1.Condition{Status: metav1.ConditionFalse, Reason: ocv1.ReasonProgressDeadlineExceeded, Message: "deadline exceeded"},
		},
		{
			name: "deployment invalid specification maps to Blocked",
			cod:  &metav1.Condition{Status: metav1.ConditionFalse, Reason: ocv1.ClusterObjectDeploymentReasonInvalidSpecification, Message: "bad template metadata"},
			want: metav1.Condition{Status: metav1.ConditionFalse, Reason: ocv1.ReasonBlocked, Message: "bad template metadata"},
		},
		{
			name: "revision invalid specification maps to Blocked",
			cod:  &metav1.Condition{Status: metav1.ConditionFalse, Reason: ocv1.ClusterObjectDeploymentReasonRevisionInvalidSpecification, Message: "bad revision spec"},
			want: metav1.Condition{Status: metav1.ConditionFalse, Reason: ocv1.ReasonBlocked, Message: "bad revision spec"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := progressingConditionFromClusterObjectDeployment(tc.cod)
			require.Equal(t, ocv1.TypeProgressing, got.Type)
			require.Equal(t, tc.want.Status, got.Status, "Status")
			require.Equal(t, tc.want.Reason, got.Reason, "Reason")
			require.Equal(t, tc.want.Message, got.Message, "Message")
		})
	}
}

func TestSetAvailableFromClusterObjectDeployment(t *testing.T) {
	t.Run("available deployment re-emits ProbesSucceeded", func(t *testing.T) {
		ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Generation: 3}}
		cod := &ocv1.ClusterObjectDeployment{}
		apimeta.SetStatusCondition(&cod.Status.Conditions, metav1.Condition{
			Type: ocv1.ClusterObjectDeploymentTypeAvailable, Status: metav1.ConditionTrue,
			Reason: ocv1.ClusterObjectDeploymentReasonAvailable, Message: "an active revision is ready",
		})
		setAvailableFromClusterObjectDeployment(ext, cod)
		got := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeAvailable)
		require.NotNil(t, got)
		require.Equal(t, metav1.ConditionTrue, got.Status)
		require.Equal(t, ocv1.ReasonProbesSucceeded, got.Reason)
		require.Equal(t, availableTrueMessage, got.Message)
		require.Equal(t, int64(3), got.ObservedGeneration)
	})

	t.Run("unavailable deployment surfaces reason as-is", func(t *testing.T) {
		ext := &ocv1.ClusterExtension{}
		cod := &ocv1.ClusterObjectDeployment{}
		apimeta.SetStatusCondition(&cod.Status.Conditions, metav1.Condition{
			Type: ocv1.ClusterObjectDeploymentTypeAvailable, Status: metav1.ConditionFalse,
			Reason: ocv1.ClusterObjectDeploymentReasonUnavailable, Message: "no active revision is ready",
		})
		setAvailableFromClusterObjectDeployment(ext, cod)
		got := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeAvailable)
		require.NotNil(t, got)
		require.Equal(t, metav1.ConditionFalse, got.Status)
		require.Equal(t, ocv1.ClusterObjectDeploymentReasonUnavailable, got.Reason)
	})

	t.Run("missing deployment Available condition leaves extension unchanged", func(t *testing.T) {
		ext := &ocv1.ClusterExtension{}
		setAvailableFromClusterObjectDeployment(ext, &ocv1.ClusterObjectDeployment{})
		require.Nil(t, apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeAvailable))
	})
}

func TestClusterObjectDeploymentRolloutComplete(t *testing.T) {
	t.Run("complete at current generation", func(t *testing.T) {
		require.True(t, clusterObjectDeploymentRolloutComplete(
			codWithProgressing("ext", 2, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, "")))
	})
	t.Run("not complete while rolling out", func(t *testing.T) {
		require.False(t, clusterObjectDeploymentRolloutComplete(
			codWithProgressing("ext", 2, metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, "")))
	})
	t.Run("not complete when condition observed a stale generation", func(t *testing.T) {
		cod := codWithProgressing("ext", 5, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, "")
		// Template advanced to generation 6 but the "progressed" condition is still from generation 5.
		cod.Generation = 6
		require.False(t, clusterObjectDeploymentRolloutComplete(cod))
	})
	t.Run("not complete when no Progressing condition", func(t *testing.T) {
		require.False(t, clusterObjectDeploymentRolloutComplete(&ocv1.ClusterObjectDeployment{}))
	})
}

func TestBundleMetadataFromClusterObjectDeployment(t *testing.T) {
	cod := &ocv1.ClusterObjectDeployment{}
	cod.Spec.Template.Metadata.Annotations = map[string]string{
		labels.BundleNameKey:    "test.1.2.0",
		labels.BundleVersionKey: "1.2.0",
		labels.BundleReleaseKey: "2",
	}
	bm := bundleMetadataFromClusterObjectDeployment(cod)
	require.Equal(t, "test.1.2.0", bm.Name)
	require.Equal(t, "1.2.0", bm.Version)
	require.Equal(t, ptr.To("2"), bm.Release)

	t.Run("release omitted when annotation absent", func(t *testing.T) {
		cod := &ocv1.ClusterObjectDeployment{}
		cod.Spec.Template.Metadata.Annotations = map[string]string{labels.BundleNameKey: "x", labels.BundleVersionKey: "1"}
		require.Nil(t, bundleMetadataFromClusterObjectDeployment(cod).Release)
	})
}

func TestSetInstalledFromClusterObjectDeployment(t *testing.T) {
	bundleAnnotations := map[string]string{labels.BundleNameKey: "test.1.2.0", labels.BundleVersionKey: "1.2.0"}

	t.Run("completed rollout promotes the deployment template bundle", func(t *testing.T) {
		ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
		cod := codWithProgressing("ext", 1, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, "")
		cod.Spec.Template.Metadata.Annotations = map[string]string{
			labels.BundleNameKey:      "test.1.2.0",
			labels.BundleVersionKey:   "1.2.0",
			labels.BundleReferenceKey: "registry.example/test@sha256:abc",
		}
		setInstalledFromClusterObjectDeployment(ext, cod)
		require.NotNil(t, ext.Status.Install)
		require.Equal(t, "test.1.2.0", ext.Status.Install.Bundle.Name)
		installed := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		require.Equal(t, metav1.ConditionTrue, installed.Status)
		// The message surfaces the image reference (bundle-reference annotation), not the name.
		require.Equal(t, "Installed bundle registry.example/test@sha256:abc successfully", installed.Message)
	})

	t.Run("completed rollout falls back to the bundle name when no image reference", func(t *testing.T) {
		ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
		cod := codWithProgressing("ext", 1, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, "")
		cod.Spec.Template.Metadata.Annotations = bundleAnnotations
		setInstalledFromClusterObjectDeployment(ext, cod)
		installed := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		require.Equal(t, metav1.ConditionTrue, installed.Status)
		require.Equal(t, "Installed bundle test.1.2.0 successfully", installed.Message)
	})

	t.Run("in-progress first install reports not installed", func(t *testing.T) {
		ext := &ocv1.ClusterExtension{}
		cod := codWithProgressing("ext", 1, metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, "")
		setInstalledFromClusterObjectDeployment(ext, cod)
		require.Nil(t, ext.Status.Install)
		installed := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		require.Equal(t, metav1.ConditionFalse, installed.Status)
		require.Equal(t, ocv1.ReasonAbsent, installed.Reason)
	})

	t.Run("in-progress upgrade keeps the previously installed bundle", func(t *testing.T) {
		// A prior completion recorded v1.2.0; a new revision (v1.3.0) is now rolling out.
		ext := &ocv1.ClusterExtension{
			Status: ocv1.ClusterExtensionStatus{Install: &ocv1.ClusterExtensionInstallStatus{
				Bundle: ocv1.BundleMetadata{Name: "test.1.2.0", Version: "1.2.0"},
			}},
		}
		cod := codWithProgressing("ext", 2, metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, "")
		cod.Spec.Template.Metadata.Annotations = map[string]string{labels.BundleNameKey: "test.1.3.0", labels.BundleVersionKey: "1.3.0"}
		setInstalledFromClusterObjectDeployment(ext, cod)
		require.Equal(t, "test.1.2.0", ext.Status.Install.Bundle.Name, "installed bundle must not advance until the rollout completes")
	})

	t.Run("blocked first install reports Failed", func(t *testing.T) {
		ext := &ocv1.ClusterExtension{}
		cod := codWithProgressing("ext", 1, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonRevisionObjectCollision, "collision")
		setInstalledFromClusterObjectDeployment(ext, cod)
		require.Equal(t, ocv1.ReasonFailed, apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled).Reason)
	})
}

func TestReflectClusterObjectDeploymentStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))

	t.Run("maps a completed deployment onto the extension", func(t *testing.T) {
		cod := codWithProgressing("test-ext", 1, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, "latest revision has progressed")
		apimeta.SetStatusCondition(&cod.Status.Conditions, metav1.Condition{
			Type: ocv1.ClusterObjectDeploymentTypeAvailable, Status: metav1.ConditionTrue,
			Reason: ocv1.ClusterObjectDeploymentReasonAvailable, ObservedGeneration: 1,
		})
		cod.Spec.Template.Metadata.Annotations = map[string]string{labels.BundleNameKey: "test.1.2.0", labels.BundleVersionKey: "1.2.0"}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cod).Build()
		ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext", Generation: 1}}

		res, err := ReflectClusterObjectDeploymentStatus(cl)(t.Context(), &reconcileState{}, ext)
		require.NoError(t, err)
		require.Nil(t, res)
		require.Equal(t, ocv1.ReasonProbesSucceeded, apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeAvailable).Reason)
		require.Equal(t, ocv1.ReasonSucceeded, apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing).Reason)
		require.Equal(t, "test.1.2.0", ext.Status.Install.Bundle.Name)
	})

	t.Run("absent deployment reports rolling out and not installed", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext"}}

		res, err := ReflectClusterObjectDeploymentStatus(cl)(t.Context(), &reconcileState{}, ext)
		require.NoError(t, err)
		require.Nil(t, res)
		require.Equal(t, ocv1.ReasonRollingOut, apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing).Reason)
		require.Equal(t, metav1.ConditionFalse, apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled).Status)
	})
}
