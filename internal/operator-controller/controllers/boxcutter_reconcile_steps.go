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
	"fmt"
	"io/fs"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/revisions"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

// TODO(COD): Read revision state from ClusterObjectDeployment status instead of
// discovering COS revisions directly from ClusterExtension reconciliation.
type BoxcutterRevisionStatesGetter struct {
	Reader client.Reader
}

func (d *BoxcutterRevisionStatesGetter) GetRevisionStates(ctx context.Context, ext *ocv1.ClusterExtension) (*RevisionStates, error) {
	rs := &RevisionStates{}
	cod := &ocv1.ClusterObjectDeployment{}
	if err := d.Reader.Get(ctx, client.ObjectKey{Name: ext.Name}, cod); err != nil {
		if apierrors.IsNotFound(err) {
			// Without a COD, there is no revision state this ClusterExtension should report.
			return rs, nil
		}
		return nil, fmt.Errorf("getting ClusterObjectDeployment %q: %w", ext.Name, err)
	}
	if !metav1.IsControlledBy(cod, ext) {
		// Do not report status for a deployment owned by someone else, even if it
		// has the name this ClusterExtension expects to use.
		return rs, nil
	}

	existingRevisions, err := revisions.ListForGroup(ctx, d.Reader, ext.Name)
	if err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}

	for _, rev := range existingRevisions {
		if rev.Spec.LifecycleState == ocv1.ClusterObjectSetLifecycleStateArchived {
			continue
		}

		// Group membership is based on the name and does not prove ownership.
		// Ignore revisions outside this deployment's ownership chain.
		if !metav1.IsControlledBy(&rev, cod) {
			continue
		}

		// TODO: the setting of these annotations (happens in boxcutter applier when we pass in "revisionAnnotations")
		//   is fairly decoupled from this code where we get the annotations back out. We may want to co-locate
		//   the set/get logic a bit better to make it more maintainable and less likely to get out of sync.
		rm := &RevisionMetadata{
			RevisionName: rev.Name,
			Package:      rev.Annotations[labels.PackageNameKey],
			Image:        rev.Annotations[labels.BundleReferenceKey],
			Conditions:   rev.Status.Conditions,
			BundleMetadata: ocv1.BundleMetadata{
				Name:    rev.Annotations[labels.BundleNameKey],
				Version: rev.Annotations[labels.BundleVersionKey],
			},
		}
		// Only set Release if the annotation key exists (to distinguish "not set" from "explicitly empty")
		if releaseValue, ok := rev.Annotations[labels.BundleReleaseKey]; ok {
			rm.Release = &releaseValue
		}

		// A revision is considered installed once it has been observed ready at
		// least once, recorded by status.completedAt.
		if !rev.Status.CompletedAt.IsZero() {
			rs.Installed = rm
		} else {
			rs.RollingOut = append(rs.RollingOut, rm)
		}
	}

	return rs, nil
}

// TODO(OPRUN-4810): Rework Helm storage migration for ClusterObjectDeployment
// before re-enabling it. Preserve the existing migration implementation for that work.
const helmStorageMigrationEnabled = false

func MigrateStorage(m StorageMigrator) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		if !helmStorageMigrationEnabled {
			return nil, nil
		}

		objLbls := map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.GetName(),
		}

		if err := m.Migrate(ctx, ext, objLbls); err != nil {
			return nil, fmt.Errorf("migrating storage: %w", err)
		}
		return nil, nil
	}
}

func ApplyBundleWithBoxcutter(apply func(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (bool, string, error)) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)
		revisionAnnotations := map[string]string{
			labels.BundleNameKey:      state.resolvedRevisionMetadata.Name,
			labels.PackageNameKey:     state.resolvedRevisionMetadata.Package,
			labels.BundleVersionKey:   state.resolvedRevisionMetadata.Version,
			labels.BundleReferenceKey: state.resolvedRevisionMetadata.Image,
		}
		if state.resolvedRevisionMetadata.Release != nil {
			revisionAnnotations[labels.BundleReleaseKey] = *state.resolvedRevisionMetadata.Release
		}
		objLbls := map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.GetName(),
		}

		l.Info("applying bundle contents")
		_, _, err := apply(ctx, state.imageFS, ext, objLbls, revisionAnnotations)
		if err != nil {
			// If there was an error applying the resolved bundle,
			// report the error via the Progressing condition.
			setStatusProgressing(ext, wrapErrorWithResolutionInfo(state.resolvedRevisionMetadata.BundleMetadata, err))
			// Only set Installed condition for retryable errors.
			// For terminal errors (Progressing: False with a terminal reason such as Blocked or InvalidConfiguration),
			// the Progressing condition already provides all necessary information about the failure.
			if !errors.Is(err, reconcile.TerminalError(nil)) {
				setInstalledStatusFromRevisionStates(ext, state.revisionStates)
			}
			return nil, err
		}

		// Success-path status (Available, Progressing, Installed) is reflected from the
		// ClusterObjectDeployment by the ReflectClusterObjectDeploymentStatus step.
		return nil, nil
	}
}

// ReflectClusterObjectDeploymentStatus reads the ClusterObjectDeployment the applier manages for the
// ClusterExtension and maps its rolled-up Available, Progressing, and installed-bundle state onto the
// ClusterExtension. The deployment is the single object the ClusterExtension observes for rollout
// health, replacing per-ClusterObjectSet observation.
func ReflectClusterObjectDeploymentStatus(reader client.Reader) ReconcileStepFunc {
	return func(ctx context.Context, _ *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		cod := &ocv1.ClusterObjectDeployment{}
		switch err := reader.Get(ctx, client.ObjectKey{Name: ext.Name}, cod); {
		case apierrors.IsNotFound(err):
			// The deployment was just applied and is not yet visible in the cache; its status is empty
			// anyway. Mapping from an empty deployment reports "rolling out"; the watch re-triggers
			// reconciliation once the deployment and its status become visible.
			cod = &ocv1.ClusterObjectDeployment{}
		case err != nil:
			return nil, fmt.Errorf("getting ClusterObjectDeployment %q: %w", ext.Name, err)
		}
		setAvailableFromClusterObjectDeployment(ext, cod)
		setProgressingFromClusterObjectDeployment(ext, cod)
		setInstalledFromClusterObjectDeployment(ext, cod)
		return nil, nil
	}
}
