package controllers

import (
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

// The functions in this file map a ClusterObjectDeployment's rolled-up status onto the owning
// ClusterExtension's conditions. The ClusterObjectDeployment is the single object the ClusterExtension
// observes for rollout health: its Available and Progressing conditions aggregate the health of the
// underlying ClusterObjectSet revisions (including the progress deadline and head-revision blocking
// reasons), and its template carries the bundle identity.

// availableTrueMessage is the message surfaced on the ClusterExtension Available condition when the
// ClusterObjectDeployment is available. It is intentionally decoupled from the deployment's own message.
const availableTrueMessage = "Objects are available and pass all probes."

// setAvailableFromClusterObjectDeployment maps the deployment's Available condition onto the
// ClusterExtension's Available condition. An available (True) deployment is re-emitted with a
// ClusterExtension-owned reason and message; other states are surfaced as-is with the Type rewritten.
func setAvailableFromClusterObjectDeployment(ext *ocv1.ClusterExtension, cod *ocv1.ClusterObjectDeployment) {
	available := apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeAvailable)
	if available == nil {
		// The deployment has not reported availability yet; leave any prior value in place.
		return
	}
	a := *available
	a.Type = ocv1.TypeAvailable
	a.ObservedGeneration = ext.GetGeneration()
	if a.Status == metav1.ConditionTrue {
		a.Reason = ocv1.ReasonProbesSucceeded
		a.Message = availableTrueMessage
	}
	apimeta.SetStatusCondition(&ext.Status.Conditions, a)
}

// setProgressingFromClusterObjectDeployment maps the deployment's Progressing condition onto the
// ClusterExtension's Progressing condition.
func setProgressingFromClusterObjectDeployment(ext *ocv1.ClusterExtension, cod *ocv1.ClusterObjectDeployment) {
	prog := progressingConditionFromClusterObjectDeployment(
		apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeProgressing))
	prog.ObservedGeneration = ext.GetGeneration()
	apimeta.SetStatusCondition(&ext.Status.Conditions, prog)
}

// progressingConditionFromClusterObjectDeployment translates a ClusterObjectDeployment Progressing
// condition into a ClusterExtension Progressing condition. The ClusterExtension follows the OLMv1
// convention where a completed rollout stays Progressing=True/Succeeded and only terminal states are
// False, so the status is recomputed per reason rather than copied from the deployment. A nil deployment
// condition (e.g. a freshly created deployment that has not reconciled) is treated as rolling out.
func progressingConditionFromClusterObjectDeployment(codProgressing *metav1.Condition) metav1.Condition {
	prog := metav1.Condition{Type: ocv1.TypeProgressing, Status: metav1.ConditionTrue}
	if codProgressing == nil {
		prog.Reason = ocv1.ReasonRollingOut
		prog.Message = "Revision is rolling out."
		return prog
	}
	prog.Message = codProgressing.Message
	switch codProgressing.Reason {
	case ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed:
		// The deployment reports a completed rollout as Progressing=False; the ClusterExtension keeps
		// Progressing=True/Succeeded to signal "reconciled, desired state reached".
		prog.Reason = ocv1.ReasonSucceeded
		prog.Message = "Desired state reached"
	case ocv1.ClusterObjectDeploymentReasonRevisionObjectCollision,
		ocv1.ClusterObjectDeploymentReasonRevisionPreconditionsNotMet:
		// The head revision is impeded by a foreign-owned object or an unmet precondition. These clear
		// on their own once the obstruction is resolved, so the extension keeps retrying.
		prog.Reason = ocv1.ReasonRetrying
	case ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded:
		prog.Status = metav1.ConditionFalse
		prog.Reason = ocv1.ReasonProgressDeadlineExceeded
	case ocv1.ClusterObjectDeploymentReasonInvalidSpecification,
		ocv1.ClusterObjectDeploymentReasonRevisionInvalidSpecification:
		// An invalid deployment template or revision spec needs a spec change to proceed; it does not
		// retry on its own, so report it as terminal.
		prog.Status = metav1.ConditionFalse
		prog.Reason = ocv1.ReasonBlocked
	default:
		// NewClusterObjectSetProgressing and any future rolling reason.
		prog.Reason = ocv1.ReasonRollingOut
	}
	return prog
}

// setInstalledFromClusterObjectDeployment maps the deployment onto the ClusterExtension's Installed
// condition and .status.install. The installed bundle is only promoted once the current rollout has
// completed; while a rollout is in flight the previously-installed bundle is left untouched, so an
// in-progress upgrade keeps reporting the old bundle until the new one succeeds. Resolution is gated
// while a revision is rolling out, so the deployment template still names the just-completed bundle at
// the moment completion is observed.
func setInstalledFromClusterObjectDeployment(ext *ocv1.ClusterExtension, cod *ocv1.ClusterObjectDeployment) {
	if clusterObjectDeploymentRolloutComplete(cod) {
		bundle := bundleMetadataFromClusterObjectDeployment(cod)
		setInstallStatus(ext, &ocv1.ClusterExtensionInstallStatus{Bundle: bundle})
		// Surface the resolved bundle image reference in the message, matching the
		// pre-ClusterObjectDeployment status which reported the image (the bundle-reference
		// annotation) rather than the bundle name. BundleMetadata has no image field, so read
		// the annotation the applier stamps onto the template; fall back to the bundle name if
		// it is absent (e.g. a revision created without the annotation).
		installed := bundle.Name
		if ref := cod.Spec.Template.Metadata.Annotations[labels.BundleReferenceKey]; ref != "" {
			installed = ref
		}
		setInstalledStatusConditionSuccess(ext, fmt.Sprintf("Installed bundle %s successfully", installed))
		return
	}
	if ext.Status.Install == nil {
		// Nothing has ever been installed (first install in progress or failing).
		setInstalledStatusConditionFalse(ext, installFailureReasonFromClusterObjectDeployment(cod), "No bundle installed")
	}
	// Otherwise a bundle is already installed (from a prior completion) and a new rollout is in flight;
	// leave Installed=True and .status.install unchanged until the rollout completes.
}

// clusterObjectDeploymentRolloutComplete reports whether the deployment's current-generation rollout
// has finished successfully. ObservedGeneration is checked so a stale "progressed" condition from a
// prior template generation is not mistaken for completion of the current one.
func clusterObjectDeploymentRolloutComplete(cod *ocv1.ClusterObjectDeployment) bool {
	prog := apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeProgressing)
	return prog != nil &&
		prog.Status == metav1.ConditionFalse &&
		prog.Reason == ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed &&
		prog.ObservedGeneration == cod.Generation
}

// bundleMetadataFromClusterObjectDeployment extracts the bundle identity the applier stamped onto the
// deployment's template metadata. During a rollout this names the bundle being rolled out, so callers
// must only promote it to .status.install once the rollout is complete.
func bundleMetadataFromClusterObjectDeployment(cod *ocv1.ClusterObjectDeployment) ocv1.BundleMetadata {
	ann := cod.Spec.Template.Metadata.Annotations
	bm := ocv1.BundleMetadata{
		Name:    ann[labels.BundleNameKey],
		Version: ann[labels.BundleVersionKey],
	}
	if release, ok := ann[labels.BundleReleaseKey]; ok {
		bm.Release = &release
	}
	return bm
}

// installFailureReasonFromClusterObjectDeployment derives the Installed=False reason for a
// not-yet-installed extension from the deployment's Progressing reason: a blocking reason maps to
// Failed, while a healthy in-progress rollout maps to Absent.
func installFailureReasonFromClusterObjectDeployment(cod *ocv1.ClusterObjectDeployment) string {
	prog := apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeProgressing)
	if prog != nil {
		switch prog.Reason {
		case ocv1.ClusterObjectDeploymentReasonRevisionObjectCollision,
			ocv1.ClusterObjectDeploymentReasonRevisionPreconditionsNotMet,
			ocv1.ClusterObjectDeploymentReasonRevisionInvalidSpecification,
			ocv1.ClusterObjectDeploymentReasonInvalidSpecification,
			ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded:
			return ocv1.ReasonFailed
		}
	}
	return ocv1.ReasonAbsent
}
