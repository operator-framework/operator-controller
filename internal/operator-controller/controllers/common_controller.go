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
		reason := determineFailureReason(revisionStates.RollingOut)
		setInstalledStatusConditionFalse(ext, reason, "No bundle installed")
		return
	}
	// Something is installed
	installStatus := &ocv1.ClusterExtensionInstallStatus{
		Bundle: revisionStates.Installed.BundleMetadata,
	}
	setInstallStatus(ext, installStatus)
	setInstalledStatusConditionSuccess(ext, fmt.Sprintf("Installed bundle %s successfully", revisionStates.Installed.Image))
}

// setActiveRevisionsFromRevisionStates derives the active revisions for the ClusterExtension status
func setActiveRevisionsFromRevisionStates(ext *ocv1.ClusterExtension, revisionStates *RevisionStates) {
	ext.Status.ActiveRevisions = make([]ocv1.RevisionStatus, 0, 1+len(revisionStates.RollingOut))
	if i := revisionStates.Installed; i != nil {
		ext.Status.ActiveRevisions = append(ext.Status.ActiveRevisions, ocv1.RevisionStatus{Name: i.RevisionName})
	}
	for _, r := range revisionStates.RollingOut {
		rs := ocv1.RevisionStatus{Name: r.RevisionName}
		avail := apimeta.FindStatusCondition(r.Conditions, ocv1.ClusterObjectSetTypeReady)
		if avail != nil {
			a := *avail
			a.Type = ocv1.TypeAvailable
			a.ObservedGeneration = ext.GetGeneration()
			apimeta.SetStatusCondition(&rs.Conditions, a)
		}
		ext.Status.ActiveRevisions = append(ext.Status.ActiveRevisions, rs)
	}
}

// setAvailableFromRevisionStates sets the Available status condition based on the given revision states
func setAvailableFromRevisionStates(ext *ocv1.ClusterExtension, revisionStates *RevisionStates) {
	if i := revisionStates.Installed; i != nil {
		avail := apimeta.FindStatusCondition(i.Conditions, ocv1.ClusterObjectSetTypeReady)
		if avail != nil {
			a := *avail
			a.Type = ocv1.TypeAvailable
			a.ObservedGeneration = ext.GetGeneration()
			apimeta.SetStatusCondition(&ext.Status.Conditions, a)
		}
	}
}

// setProgressingFromRevisionStates sets the Progressing status condition based on the given revision states
// The Progressing condition is derived from the Available condition of either:
// - the latest rolling out revision (when one exists); OR
// - the installed revision
// When the source revision has no Available condition yet (e.g. a freshly created revision that has
// not reconciled), a nil condition is passed so the helper applies the RollingOut default rather than
// leaving Progressing stale.
func setProgressingFromRevisionStates(ext *ocv1.ClusterExtension, revisionStates *RevisionStates) {
	if len(revisionStates.RollingOut) > 0 {
		revisionMeta := revisionStates.RollingOut[len(revisionStates.RollingOut)-1]
		avail := apimeta.FindStatusCondition(revisionMeta.Conditions, ocv1.ClusterObjectSetTypeReady)
		setProgressingFromAvailable(ext, avail, false)
	} else if revisionStates.Installed != nil {
		setProgressingFromAvailable(ext, apimeta.FindStatusCondition(revisionStates.Installed.Conditions, ocv1.ClusterObjectSetTypeReady), true)
	}
}

// setProgressingFromAvailable derives the correct Progressing condition for the ClusterExtension from the given
// revision Available condition, and whether the revision is complete or not
func setProgressingFromAvailable(ext *ocv1.ClusterExtension, availableCond *metav1.Condition, isRevisionCompleted bool) {
	prog := progressingFromAvailable(availableCond, isRevisionCompleted)
	prog.ObservedGeneration = ext.GetGeneration()
	apimeta.SetStatusCondition(&ext.Status.Conditions, prog)
}

// determineFailureReason determines the appropriate reason for the Installed condition
// when no bundle is installed (Installed: False).
//
// Returns Failed when:
//   - No rolling revisions exist (nothing to install)
//   - The latest rolling revision has Available condition with Reason: Reconciling (indicates an error occurred)
//
// Returns Absent when:
//   - Rolling revisions exist with the latest not having Available=Reconciling (healthy phased rollout in progress)
//
// Rationale:
//   - Failed: Semantically indicates an error prevented installation
//   - Absent: Semantically indicates "not there yet" (neutral state, e.g., during healthy rollout)
//   - Reconciling reason on Available indicates an error (config validation, apply failure, etc.)
//   - Other Available reasons indicate healthy progress or terminal states handled elsewhere
//   - Only the LATEST revision matters - old errors superseded by newer healthy revisions should not cause Failed
//
// Note: rollingRevisions are sorted in ascending order by Spec.Revision (oldest to newest),
// so the latest revision is the LAST element in the array.
func determineFailureReason(rollingRevisions []*RevisionMetadata) string {
	if len(rollingRevisions) == 0 {
		return ocv1.ReasonFailed
	}
	// Latest revision is the last element (sorted ascending by Spec.Revision).
	latestRevision := rollingRevisions[len(rollingRevisions)-1]
	availableCond := apimeta.FindStatusCondition(latestRevision.Conditions, ocv1.ClusterObjectSetTypeReady)
	// Reconciling is the new home of the old Retrying signal: it indicates an error occurred.
	if availableCond != nil && availableCond.Reason == ocv1.ClusterObjectSetReasonReconciling {
		return ocv1.ReasonFailed
	}

	// No error detected in latest revision - it's progressing healthily (RollingOut) or no conditions set
	// Use Absent for neutral "not installed yet" state
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
		progressingCond.Message = errorutil.SanitizeNetworkError(errorutil.UnwrapTerminal(err))
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

// progressingFromAvailable reconstructs the ClusterExtension Progressing condition
// for a single revision from that revision's ClusterObjectSet Available condition
// and whether the revision has completed its rollout (status.completedAt set).
//
// This replaces the previous behavior of mirroring the ClusterObjectSet Progressing
// condition, which no longer exists. ObservedGeneration is left unset; callers set it.
func progressingFromAvailable(available *metav1.Condition, completed bool) metav1.Condition {
	cond := metav1.Condition{
		Type:   ocv1.TypeProgressing,
		Status: metav1.ConditionTrue,
	}
	if completed {
		cond.Reason = ocv1.ReasonSucceeded
		cond.Message = "Desired state reached"
		return cond
	}
	if available == nil {
		cond.Reason = ocv1.ReasonRollingOut
		cond.Message = "Revision is rolling out."
		return cond
	}
	cond.Message = available.Message
	switch available.Reason {
	case ocv1.ClusterObjectSetReasonBlocked:
		cond.Status = metav1.ConditionFalse
		cond.Reason = ocv1.ReasonBlocked
	case ocv1.ReasonProgressDeadlineExceeded:
		cond.Status = metav1.ConditionFalse
		cond.Reason = ocv1.ReasonProgressDeadlineExceeded
	case ocv1.ClusterObjectSetReasonReconciling:
		cond.Reason = ocv1.ReasonRetrying
	default:
		// ProbeFailure, RollingOut, or ProbesSucceeded-but-not-yet-complete.
		cond.Reason = ocv1.ReasonRollingOut
	}
	return cond
}
