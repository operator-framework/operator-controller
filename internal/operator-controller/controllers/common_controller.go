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

const (
	// maxConditionMessageLength set the max message length allowed by Kubernetes.
	maxConditionMessageLength = 32768
	// truncationSuffix is the suffix added when a message is cut.
	truncationSuffix = "\n\n... [message truncated]"
)

// truncateMessage cuts long messages to fit Kubernetes condition limits
func truncateMessage(message string) string {
	if len(message) <= maxConditionMessageLength {
		return message
	}

	maxContent := maxConditionMessageLength - len(truncationSuffix)
	return message[:maxContent] + truncationSuffix
}

// SetStatusCondition wraps apimeta.SetStatusCondition and ensures the message is always truncated
// This should be used throughout the codebase instead of apimeta.SetStatusCondition directly
func SetStatusCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	condition.Message = truncateMessage(condition.Message)
	apimeta.SetStatusCondition(conditions, condition)
}

// setInstalledStatusFromBundle sets the installed status based on the given installedBundle.
func setInstalledStatusFromBundle(ext *ocv1.ClusterExtension, installedBundle *InstalledBundle) {
	// Nothing is installed
	if installedBundle == nil {
		setInstallStatus(ext, nil)
		setInstalledStatusConditionFailed(ext, "No bundle installed")
		return
	}
	// Something is installed
	installStatus := &ocv1.ClusterExtensionInstallStatus{
		Bundle: installedBundle.BundleMetadata,
	}
	setInstallStatus(ext, installStatus)
	setInstalledStatusConditionSuccess(ext, fmt.Sprintf("Installed bundle %s successfully", installedBundle.Image))
}

// setInstalledStatusConditionSuccess sets the installed status condition to success.
func setInstalledStatusConditionSuccess(ext *ocv1.ClusterExtension, message string) {
	SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1.TypeInstalled,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1.ReasonSucceeded,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setInstalledStatusConditionFailed sets the installed status condition to failed.
func setInstalledStatusConditionFailed(ext *ocv1.ClusterExtension, message string) {
	SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1.TypeInstalled,
		Status:             metav1.ConditionFalse,
		Reason:             ocv1.ReasonFailed,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setInstalledStatusConditionUnknown sets the installed status condition to unknown.
func setInstalledStatusConditionUnknown(ext *ocv1.ClusterExtension, message string) {
	SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
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
		Message:            "desired state reached",
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

	SetStatusCondition(&ext.Status.Conditions, progressingCond)
}
