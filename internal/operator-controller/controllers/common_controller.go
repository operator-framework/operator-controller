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
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// setInstalledStatusFromRevisionStates sets the installed status based on the given installedBundle.
func setInstalledStatusFromRevisionStates(ext *ocv1.ClusterExtension, revisionStates *RevisionStates) {
	// Nothing is installed
	if revisionStates.Installed == nil {
		setInstallStatus(ext, nil)
		if len(revisionStates.RollingOut) == 0 {
			setInstalledStatusConditionFalse(ext, ocv1.ReasonFailed, "No bundle installed")
		} else {
			setInstalledStatusConditionFalse(ext, ocv1.ReasonAbsent, "No bundle installed")
		}
		return
	}
	// Something is installed
	installStatus := &ocv1.ClusterExtensionInstallStatus{
		Bundle: revisionStates.Installed.BundleMetadata,
	}
	setInstallStatus(ext, installStatus)
	setInstalledStatusConditionSuccess(ext, fmt.Sprintf("Installed bundle %s successfully", revisionStates.Installed.Image))
}

// setInstalledStatusConditionSuccess sets the installed status condition to success.
func setInstalledStatusConditionSuccess(ext *ocv1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1.TypeInstalled,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1.ReasonSucceeded,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setInstalledStatusConditionFailed sets the installed status condition to failed.
func setInstalledStatusConditionFalse(ext *ocv1.ClusterExtension, reason string, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1.TypeInstalled,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setInstalledStatusConditionUnknown sets the installed status condition to unknown.
func setInstalledStatusConditionUnknown(ext *ocv1.ClusterExtension, message string) {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1.TypeInstalled,
		Status:             metav1.ConditionUnknown,
		Reason:             ocv1.ReasonFailed,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

func setInstallStatus(ext *ocv1.ClusterExtension, installStatus *ocv1.ClusterExtensionInstallStatus) {
	ext.Status.Install = installStatus
}

func setStatusProgressing(ext *ocv1.ClusterExtension, err error) {
	progressingCond := metav1.Condition{
		Type:               ocv1.TypeProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1.ReasonSucceeded,
		Message:            "Desired state reached",
		ObservedGeneration: ext.GetGeneration(),
	}

	if err != nil {
		progressingCond.Reason = ocv1.ReasonRetrying
		progressingCond.Message = err.Error()
	}

	if errors.Is(err, reconcile.TerminalError(nil)) {
		progressingCond.Status = metav1.ConditionFalse
		progressingCond.Reason = ocv1.ReasonBlocked
	}

	apimeta.SetStatusCondition(&ext.Status.Conditions, progressingCond)
}
