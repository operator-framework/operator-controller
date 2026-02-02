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
	errorutil "github.com/operator-framework/operator-controller/internal/shared/util/error"
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

// setInstalledStatusFromRevisionStates sets the installed status based on the given installedBundle.
func setInstalledStatusFromRevisionStates(ext *ocv1.ClusterExtension, revisionStates *RevisionStates) {
	// Nothing is installed
	if revisionStates.Installed == nil {
		setInstallStatus(ext, nil)
		reason := determineInstalledReason(revisionStates.RollingOut)
		setInstalledStatusConditionFalse(ext, reason, "No bundle installed")
		return
	}

	// Something is installed - check if upgrade is in progress
	installStatus := &ocv1.ClusterExtensionInstallStatus{
		Bundle: revisionStates.Installed.BundleMetadata,
	}
	setInstallStatus(ext, installStatus)

	if len(revisionStates.RollingOut) > 0 {
		latestRevision := revisionStates.RollingOut[len(revisionStates.RollingOut)-1]
		progressingCond := apimeta.FindStatusCondition(latestRevision.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)

		if progressingCond != nil && progressingCond.Reason == string(ocv1.ReasonRollingOut) {
			setInstalledStatusConditionUpgrading(ext, fmt.Sprintf("Upgrading from %s", revisionStates.Installed.Image))
			return
		}
	}

	setInstalledStatusConditionSuccess(ext, fmt.Sprintf("Installed bundle %s successfully", revisionStates.Installed.Image))
}

// determineInstalledReason determines the appropriate reason for the Installed condition
// when no bundle is installed (Installed: False).
//
// Returns Failed when:
//   - No rolling revisions exist (nothing to install)
//   - The latest rolling revision has Reason: Retrying (indicates an error occurred)
//
// Returns Installing when:
//   - The latest rolling revision explicitly has Reason: RollingOut (healthy installation in progress)
//
// Returns Absent when:
//   - Rolling revisions exist but have no conditions set (rollout just started)
//
// Rationale:
//   - Failed: Semantically indicates an error prevented installation
//   - Installing: Semantically indicates a first-time installation is actively in progress
//   - Absent: Neutral state when rollout exists but hasn't progressed enough to determine health
//   - Retrying reason indicates an error (config validation, apply failure, etc.)
//   - RollingOut reason indicates confirmed healthy progress
//   - Only the LATEST revision matters - old errors superseded by newer healthy revisions should not cause Failed
//
// Note: This function is only called when Installed == nil (first-time installation scenario).
// Note: rollingRevisions are sorted in ascending order by Spec.Revision (oldest to newest),
//
//	so the latest revision is the LAST element in the array.
func determineInstalledReason(rollingRevisions []*RevisionMetadata) string {
	if len(rollingRevisions) == 0 {
		return ocv1.ReasonFailed
	}

	// Check if the LATEST rolling revision indicates an error (Retrying reason)
	// Latest revision is the last element in the array (sorted ascending by Spec.Revision)
	latestRevision := rollingRevisions[len(rollingRevisions)-1]
	progressingCond := apimeta.FindStatusCondition(latestRevision.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
	if progressingCond != nil {
		if progressingCond.Reason == string(ocv1.ClusterExtensionRevisionReasonRetrying) {
			// Retrying indicates an error occurred (config, apply, validation, etc.)
			// Use Failed for semantic correctness: installation failed due to error
			return ocv1.ReasonFailed
		}
		if progressingCond.Reason == string(ocv1.ReasonRollingOut) {
			// RollingOut indicates healthy progress is confirmed
			// Use Installing to communicate that a first-time installation is actively in progress
			return ocv1.ReasonInstalling
		}
	}

	// No progressing condition or unknown reason - rollout just started or hasn't progressed
	// Use Absent as neutral state
	return ocv1.ReasonAbsent
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

// setInstalledStatusConditionUpgrading sets the installed status condition to upgrading.
func setInstalledStatusConditionUpgrading(ext *ocv1.ClusterExtension, message string) {
	SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1.TypeInstalled,
		Status:             metav1.ConditionTrue,
		Reason:             ocv1.ReasonUpgrading,
		Message:            message,
		ObservedGeneration: ext.GetGeneration(),
	})
}

// setInstalledStatusConditionFailed sets the installed status condition to failed.
func setInstalledStatusConditionFalse(ext *ocv1.ClusterExtension, reason string, message string) {
	SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:               ocv1.TypeInstalled,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
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
		Message:            "Desired state reached",
		ObservedGeneration: ext.GetGeneration(),
	}

	if err != nil {
		progressingCond.Reason = ocv1.ReasonRetrying
		// Unwrap TerminalError to avoid "terminal error:" prefix in message
		progressingCond.Message = errorutil.UnwrapTerminal(err).Error()
	}

	if errors.Is(err, reconcile.TerminalError(nil)) {
		progressingCond.Status = metav1.ConditionFalse
		// Try to extract a specific reason from the terminal error.
		// If the error was created with NewTerminalError(reason, err), use that reason.
		// Otherwise, fall back to the generic "Blocked" reason.
		if reason, ok := errorutil.ExtractTerminalReason(err); ok {
			progressingCond.Reason = reason
		} else {
			progressingCond.Reason = ocv1.ReasonBlocked
		}
	}

	SetStatusCondition(&ext.Status.Conditions, progressingCond)
}
