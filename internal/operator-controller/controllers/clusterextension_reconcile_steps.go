/*
Copyright 2025.

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
	"errors"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
)

func HandleFinalizers(f finalizer.Finalizer) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)

		l.Info("handling finalizers")
		finalizeResult, err := f.Finalize(ctx, ext)
		if err != nil {
			setStatusProgressing(ext, err)
			return nil, err
		}
		if finalizeResult.Updated || finalizeResult.StatusUpdated {
			// On create: make sure the finalizer is applied before we do anything
			// On delete: make sure we do nothing after the finalizer is removed
			return &ctrl.Result{}, nil
		}

		if ext.GetDeletionTimestamp() != nil {
			// If we've gotten here, that means the cluster extension is being deleted, we've handled all of
			// _our_ finalizers (above), but the cluster extension is still present in the cluster, likely
			// because there are _other_ finalizers that other controllers need to handle, (e.g. the orphan
			// deletion finalizer).
			return &ctrl.Result{}, nil
		}
		return nil, nil
	}
}

func RetrieveRevisionStates(r RevisionStatesGetter) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)
		l.Info("getting installed bundle")
		revisionStates, err := r.GetRevisionStates(ctx, ext)
		if err != nil {
			setInstallStatus(ext, nil)
			var saerr *authentication.ServiceAccountNotFoundError
			if errors.As(err, &saerr) {
				setInstalledStatusConditionUnknown(ext, saerr.Error())
				setStatusProgressing(ext, errors.New("installation cannot proceed due to missing ServiceAccount"))
				return nil, err
			}
			setInstalledStatusConditionUnknown(ext, err.Error())
			setStatusProgressing(ext, errors.New("retrying to get installed bundle"))
			return nil, err
		}
		state.revisionStates = revisionStates
		return nil, nil
	}
}

func ResolveBundle(r resolve.Resolver) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)
		var resolvedRevisionMetadata *RevisionMetadata
		if len(state.revisionStates.RollingOut) == 0 {
			l.Info("resolving bundle")
			var bm *ocv1.BundleMetadata
			if state.revisionStates.Installed != nil {
				bm = &state.revisionStates.Installed.BundleMetadata
			}
			resolvedBundle, resolvedBundleVersion, resolvedDeprecation, err := r.Resolve(ctx, ext, bm)
			if err != nil {
				// Note: We don't distinguish between resolution-specific errors and generic errors
				setStatusProgressing(ext, err)
				setInstalledStatusFromRevisionStates(ext, state.revisionStates)
				ensureAllConditionsWithReason(ext, ocv1.ReasonFailed, err.Error())
				return nil, err
			}

			// set deprecation status after _successful_ resolution
			// TODO:
			//  1. It seems like deprecation status should reflect the currently installed bundle, not the resolved
			//     bundle. So perhaps we should set package and channel deprecations directly after resolution, but
			//     defer setting the bundle deprecation until we successfully install the bundle.
			//  2. If resolution fails because it can't find a bundle, that doesn't mean we wouldn't be able to find
			//     a deprecation for the ClusterExtension's spec.packageName. Perhaps we should check for a non-nil
			//     resolvedDeprecation even if resolution returns an error. If present, we can still update some of
			//     our deprecation status.
			//       - Open question though: what if different catalogs have different opinions of what's deprecated.
			//         If we can't resolve a bundle, how do we know which catalog to trust for deprecation information?
			//         Perhaps if the package shows up in multiple catalogs and deprecations don't match, we can set
			//         the deprecation status to unknown? Or perhaps we somehow combine the deprecation information from
			//         all catalogs?
			SetDeprecationStatus(ext, resolvedBundle.Name, resolvedDeprecation)
			resolvedRevisionMetadata = &RevisionMetadata{
				Package:        resolvedBundle.Package,
				Image:          resolvedBundle.Image,
				BundleMetadata: bundleutil.MetadataFor(resolvedBundle.Name, *resolvedBundleVersion),
			}
		} else {
			resolvedRevisionMetadata = state.revisionStates.RollingOut[0]
		}
		state.resolvedRevisionMetadata = resolvedRevisionMetadata
		return nil, nil
	}
}

func UnpackBundle(i imageutil.Puller, cache imageutil.Cache) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)
		l.Info("unpacking resolved bundle")
		imageFS, _, _, err := i.Pull(ctx, ext.GetName(), state.resolvedRevisionMetadata.Image, cache)
		if err != nil {
			// Wrap the error passed to this with the resolution information until we have successfully
			// installed since we intend for the progressing condition to replace the resolved condition
			// and will be removing the .status.resolution field from the ClusterExtension status API
			setStatusProgressing(ext, wrapErrorWithResolutionInfo(state.resolvedRevisionMetadata.BundleMetadata, err))
			setInstalledStatusFromRevisionStates(ext, state.revisionStates)
			return nil, err
		}
		state.imageFS = imageFS
		return nil, nil
	}
}

func ApplyBundle(a Applier) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)
		revisionAnnotations := map[string]string{
			labels.BundleNameKey:      state.resolvedRevisionMetadata.Name,
			labels.PackageNameKey:     state.resolvedRevisionMetadata.Package,
			labels.BundleVersionKey:   state.resolvedRevisionMetadata.Version,
			labels.BundleReferenceKey: state.resolvedRevisionMetadata.Image,
		}
		objLbls := map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.GetName(),
		}

		l.Info("applying bundle contents")
		// NOTE: We need to be cautious of eating errors here.
		// We should always return any error that occurs during an
		// attempt to apply content to the cluster. Only when there is
		// a verifiable reason to eat the error (i.e it is recoverable)
		// should an exception be made.
		// The following kinds of errors should be returned up the stack
		// to ensure exponential backoff can occur:
		//   - Permission errors (it is not possible to watch changes to permissions.
		//     The only way to eventually recover from permission errors is to keep retrying).
		rolloutSucceeded, rolloutStatus, err := a.Apply(ctx, state.imageFS, ext, objLbls, revisionAnnotations)

		// Set installed status
		if rolloutSucceeded {
			state.revisionStates = &RevisionStates{Installed: state.resolvedRevisionMetadata}
		} else if err == nil && state.revisionStates.Installed == nil && len(state.revisionStates.RollingOut) == 0 {
			state.revisionStates = &RevisionStates{RollingOut: []*RevisionMetadata{state.resolvedRevisionMetadata}}
		}
		setInstalledStatusFromRevisionStates(ext, state.revisionStates)

		// If there was an error applying the resolved bundle,
		// report the error via the Progressing condition.
		if err != nil {
			setStatusProgressing(ext, wrapErrorWithResolutionInfo(state.resolvedRevisionMetadata.BundleMetadata, err))
			return nil, err
		} else if !rolloutSucceeded {
			apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
				Type:               ocv1.TypeProgressing,
				Status:             metav1.ConditionTrue,
				Reason:             ocv1.ReasonRolloutInProgress,
				Message:            rolloutStatus,
				ObservedGeneration: ext.GetGeneration(),
			})
		} else {
			setStatusProgressing(ext, nil)
		}
		return nil, nil
	}
}
