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

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

// BundleProvider provides the way to retrieve a list of Bundles from a source,
// generally from a catalog client of some kind.
type BundleProvider interface {
	Bundles(ctx context.Context) ([]*catalogmetadata.Bundle, error)
}

// setResolvedStatusConditionSuccess sets the resolved status condition to success.
func setResolvedStatusConditionSuccess(conditions *[]metav1.Condition, message string, generation int64) {
	apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeResolved,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1alpha1.ReasonSuccess,
		Message:            message,
		ObservedGeneration: generation,
	})
}

// setInstalledStatusConditionUnknown sets the installed status condition to unknown.
func setInstalledStatusConditionUnknown(conditions *[]metav1.Condition, message string, generation int64) {
	apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeInstalled,
		Status:             metav1.ConditionUnknown,
		Reason:             ocv1alpha1.ReasonInstallationStatusUnknown,
		Message:            message,
		ObservedGeneration: generation,
	})
}

// setResolvedStatusConditionFailed sets the resolved status condition to failed.
func setResolvedStatusConditionFailed(conditions *[]metav1.Condition, message string, generation int64) {
	apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeResolved,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1alpha1.ReasonResolutionFailed,
		Message:            message,
		ObservedGeneration: generation,
	})
}

// setResolvedStatusConditionUnknown sets the resolved status condition to unknown.
func setResolvedStatusConditionUnknown(conditions *[]metav1.Condition, message string, generation int64) {
	apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             ocv1alpha1.ReasonResolutionUnknown,
		Message:            message,
		ObservedGeneration: generation,
	})
}

// setInstalledStatusConditionSuccess sets the installed status condition to success.
func setInstalledStatusConditionSuccess(conditions *[]metav1.Condition, message string, generation int64) {
	apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeInstalled,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1alpha1.ReasonSuccess,
		Message:            message,
		ObservedGeneration: generation,
	})
}

// setInstalledStatusConditionFailed sets the installed status condition to failed.
func setInstalledStatusConditionFailed(conditions *[]metav1.Condition, message string, generation int64) {
	apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeInstalled,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1alpha1.ReasonInstallationFailed,
		Message:            message,
		ObservedGeneration: generation,
	})
}

// setDEprecationStatusesUnknown sets the deprecation status conditions to unknown.
func setDeprecationStatusesUnknown(conditions *[]metav1.Condition, message string, generation int64) {
	conditionTypes := []string{
		ocv1alpha1.TypeDeprecated,
		ocv1alpha1.TypePackageDeprecated,
		ocv1alpha1.TypeChannelDeprecated,
		ocv1alpha1.TypeBundleDeprecated,
	}

	for _, conditionType := range conditionTypes {
		apimeta.SetStatusCondition(conditions, metav1.Condition{
			Type:               conditionType,
			Reason:             ocv1alpha1.ReasonDeprecated,
			Status:             metav1.ConditionUnknown,
			Message:            message,
			ObservedGeneration: generation,
		})
	}
}
