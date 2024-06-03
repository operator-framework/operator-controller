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

package controllers

import (
	"context"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

// BundleProvider provides the way to retrieve a list of Bundles from a source,
// generally from a catalog client of some kind.
type BundleProvider interface {
	Bundles(ctx context.Context) ([]*catalogmetadata.Bundle, error)
}

// setResolvedStatusConditionSuccess sets the resolved status condition to success.
func setResolvedStatusConditionSuccess(ext *ocv1alpha1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeResolved,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1alpha1.ReasonSuccess,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setInstalledStatusConditionUnknown sets the installed status condition to unknown.
func setInstalledStatusConditionUnknown(ext *ocv1alpha1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeInstalled,
		Status:             metav1.ConditionUnknown,
		Reason:             ocv1alpha1.ReasonInstallationStatusUnknown,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setHasValidBundleUnknown sets the valid bundle condition to unknown.
func setHasValidBundleUnknown(ext *ocv1alpha1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeHasValidBundle,
		Status:             metav1.ConditionUnknown,
		Reason:             ocv1alpha1.ReasonHasValidBundleUnknown,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setHasValidBundleFalse sets the ivalid bundle condition to false
func setHasValidBundleFailed(ext *ocv1alpha1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeHasValidBundle,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1alpha1.ReasonBundleLoadFailed,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setResolvedStatusConditionFailed sets the resolved status condition to failed.
func setResolvedStatusConditionFailed(ext *ocv1alpha1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeResolved,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1alpha1.ReasonResolutionFailed,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setInstalledStatusConditionSuccess sets the installed status condition to success.
func setInstalledStatusConditionSuccess(ext *ocv1alpha1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeInstalled,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1alpha1.ReasonSuccess,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setInstalledStatusConditionFailed sets the installed status condition to failed.
func setInstalledStatusConditionFailed(ext *ocv1alpha1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeInstalled,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1alpha1.ReasonInstallationFailed,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setDeprecationStatusesUnknown sets the deprecation status conditions to unknown.
func setDeprecationStatusesUnknown(ext *ocv1alpha1.ClusterExtension, message string) {
	conditionTypes := []string{
		ocv1alpha1.TypeDeprecated,
		ocv1alpha1.TypePackageDeprecated,
		ocv1alpha1.TypeChannelDeprecated,
		ocv1alpha1.TypeBundleDeprecated,
	}

	for _, conditionType := range conditionTypes {
		apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:               conditionType,
			Reason:             ocv1alpha1.ReasonDeprecated,
			Status:             metav1.ConditionUnknown,
			Message:            message,
			ObservedGeneration: ext.GetGeneration(),
		})
	}
}

func setStatusUnpackFailed(ext *ocv1alpha1.ClusterExtension, message string) {
	ext.Status.InstalledBundle = nil
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeUnpacked,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1alpha1.ReasonUnpackFailed,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// TODO: verify if we need to update the installBundle status or leave it as is.
func setStatusUnpackPending(ext *ocv1alpha1.ClusterExtension, message string) {
	ext.Status.InstalledBundle = nil
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeUnpacked,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1alpha1.ReasonUnpackPending,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// TODO: verify if we need to update the installBundle status or leave it as is.
func setStatusUnpacking(ext *ocv1alpha1.ClusterExtension, message string) {
	ext.Status.InstalledBundle = nil
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeUnpacked,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1alpha1.ReasonUnpacking,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

func setStatusUnpacked(ext *ocv1alpha1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeUnpacked,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1alpha1.ReasonUnpackSuccess,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}
