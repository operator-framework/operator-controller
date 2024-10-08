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
	"errors"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

// setInstalledStatusConditionSuccess sets the installed status condition to success.
func setInstalledStatusConditionSuccess(ext *ocv1alpha1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeInstalled,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1alpha1.ReasonSucceeded,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setInstalledStatusConditionFailed sets the installed status condition to failed.
func setInstalledStatusConditionFailed(ext *ocv1alpha1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1alpha1.TypeInstalled,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1alpha1.ReasonFailed,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

func setInstallStatus(ext *ocv1alpha1.ClusterExtension, installStatus *ocv1alpha1.ClusterExtensionInstallStatus) {
	ext.Status.Install = installStatus
}

func setStatusProgressing(ext *ocv1alpha1.ClusterExtension, err error) {
	progressingCond := metav1.Condition{
		Type:               ocv1alpha1.TypeProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1alpha1.ReasonSucceeded,
		Message:            "desired state reached",
		ObservedGeneration: ext.GetGeneration(),
	}

	if err != nil {
		progressingCond.Status = metav1.ConditionTrue
		progressingCond.Reason = ocv1alpha1.ReasonRetrying
		progressingCond.Message = err.Error()
	}

	if errors.Is(err, reconcile.TerminalError(nil)) {
		progressingCond.Status = metav1.ConditionFalse
		progressingCond.Reason = ocv1alpha1.ReasonBlocked
	}

	apimeta.SetStatusCondition(&ext.Status.Conditions, progressingCond)
}
