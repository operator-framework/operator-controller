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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	bsemver "github.com/blang/semver/v4"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/compare"
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

// ClusterExtensionValidator is a function that validates a ClusterExtension.
// It returns an error if validation fails. Validators are executed sequentially
// in the order they are registered.
type ClusterExtensionValidator func(context.Context, *ocv1.ClusterExtension) error

// ValidateClusterExtension returns a ReconcileStepFunc that executes all
// validators sequentially. All validators are executed even if some fail,
// and all errors are collected and returned as a joined error.
// This provides complete validation feedback in a single reconciliation cycle.
func ValidateClusterExtension(validators ...ClusterExtensionValidator) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)

		l.V(1).Info("validating cluster extension")
		var validationErrors []error
		for _, validator := range validators {
			if err := validator(ctx, ext); err != nil {
				validationErrors = append(validationErrors, err)
			}
		}

		// If there are no validation errors, continue reconciliation
		if len(validationErrors) == 0 {
			return nil, nil
		}

		// Set status conditions with the validation errors
		err := fmt.Errorf("operation cannot proceed due to the following validation error(s): %w", errors.Join(validationErrors...))
		setInstalledStatusConditionUnknown(ext, err.Error())
		setStatusProgressing(ext, err)
		return nil, err
	}
}

// ServiceAccountValidator returns a validator that checks if the specified
// ServiceAccount exists in the cluster by performing a direct Get call.
func ServiceAccountValidator(saClient corev1client.ServiceAccountsGetter) ClusterExtensionValidator {
	return func(ctx context.Context, ext *ocv1.ClusterExtension) error {
		_, err := saClient.ServiceAccounts(ext.Spec.Namespace).Get(ctx, ext.Spec.ServiceAccount.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("service account %q not found in namespace %q", ext.Spec.ServiceAccount.Name, ext.Spec.Namespace)
			}
			return fmt.Errorf("error getting service account: %w", err)
		}
		return nil
	}
}

func RetrieveRevisionStates(r RevisionStatesGetter) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)
		l.Info("getting installed bundle")
		revisionStates, err := r.GetRevisionStates(ctx, ext)
		if err != nil {
			setInstallStatus(ext, nil)
			setInstalledStatusConditionUnknown(ext, err.Error())
			setStatusProgressing(ext, errors.New("retrying to get installed bundle"))
			return nil, err
		}
		state.revisionStates = revisionStates
		return nil, nil
	}
}

// ResolveBundle resolves the bundle to install or roll out for a ClusterExtension.
// It requires a controller-runtime client (in addition to the resolve.Resolver) to enable
// intelligent error handling when resolution fails. The client is used to check if ClusterCatalogs
// matching the extension's selector still exist in the cluster, allowing the controller to
// distinguish between "ClusterCatalog deleted" (fall back to installed bundle) and "transient failure"
// (retry resolution). This ensures workload resilience during ClusterCatalog outages while maintaining
// responsiveness during ClusterCatalog updates.
func ResolveBundle(r resolve.Resolver, c client.Client) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)

		// When revisions are actively rolling out, we check whether the admin has
		// changed the resolution-relevant spec fields (version, channels, selector,
		// upgradeConstraintPolicy). If so, we re-engage the resolver to honor
		// the new spec immediately.
		//
		// If the spec hasn't changed, we reuse the matching rolling-out revision
		// to avoid unnecessary catalog lookups and provide resilience during catalog outages.
		if reuseRollingOutRevision(ctx, state, ext) {
			return nil, nil
		}

		// Resolve a new bundle from the catalog
		l.V(1).Info("resolving bundle")
		var bm *ocv1.BundleMetadata
		if state.revisionStates.Installed != nil {
			bm = &state.revisionStates.Installed.BundleMetadata
		}
		resolvedBundle, resolvedBundleVersion, resolvedDeprecation, err := r.Resolve(ctx, ext, bm)

		// Get the installed bundle name for deprecation status.
		// BundleDeprecated should reflect what's currently running, not what we're trying to install.
		installedBundleName := ""
		if state.revisionStates.Installed != nil {
			installedBundleName = state.revisionStates.Installed.Name
		}

		// Set deprecation status based on resolution results:
		//  - If resolution succeeds: hasCatalogData=true, deprecation shows catalog data (nil=not deprecated)
		//  - If resolution fails but returns deprecation: hasCatalogData=true, show package/channel deprecation warnings
		//  - If resolution fails with nil deprecation: hasCatalogData=false, all conditions go Unknown
		//
		// Note: We DO check for deprecation data even when resolution fails (hasCatalogData = err == nil || resolvedDeprecation != nil).
		// This allows us to show package/channel deprecation warnings even when we can't resolve a specific bundle.
		//
		// TODO: Open question - what if different catalogs have different opinions of what's deprecated?
		//   If we can't resolve a bundle, how do we know which catalog to trust for deprecation information?
		//   Perhaps if the package shows up in multiple catalogs and deprecations don't match, we can set
		//   the deprecation status to unknown? Or perhaps we somehow combine the deprecation information from
		//   all catalogs? This needs a follow-up discussion and PR.
		hasCatalogData := err == nil || resolvedDeprecation != nil
		state.resolvedDeprecation = resolvedDeprecation
		state.hasCatalogData = hasCatalogData
		SetDeprecationStatus(ext, installedBundleName, resolvedDeprecation, hasCatalogData)

		if err != nil {
			return handleResolutionError(ctx, c, state, ext, err)
		}

		state.resolvedRevisionMetadata = &RevisionMetadata{
			Package: resolvedBundle.Package,
			Image:   resolvedBundle.Image,
			// TODO: Right now, operator-controller only supports registry+v1 bundles and has no concept
			//   of a "release" field. If/when we add a release field concept or a new bundle format
			//   we need to re-evaluate use of `AsLegacyRegistryV1Version` so that we avoid propagating
			//   registry+v1's semver spec violations of treating build metadata as orderable.
			BundleMetadata: bundleutil.MetadataFor(resolvedBundle.Name, resolvedBundleVersion.AsLegacyRegistryV1Version()),
		}
		return nil, nil
	}
}

// reuseRollingOutRevision checks whether an active rollout should continue using its
// current revision. Returns true if a matching revision was found and set on the state
// (caller should return early). Returns false if the resolver should be called instead.
//
// Re-resolution is triggered when no rolling-out revision was resolved from the same
// catalog spec as the current ClusterExtension spec. This detects any change to
// resolution-relevant fields (version, channels, selector, upgradeConstraintPolicy)
// — even when the rolling-out version still satisfies the new version constraint.
func reuseRollingOutRevision(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) bool {
	if len(state.revisionStates.RollingOut) == 0 {
		return false
	}

	l := log.FromContext(ctx)

	// RollingOut is ordered by revision number ascending (oldest first, newest last).
	latestRollingOut := state.revisionStates.RollingOut[len(state.revisionStates.RollingOut)-1]

	currentHash := CatalogSpecHash(ext)

	// Guard: if the hash can't be computed (e.g., non-catalog source), preserve the
	// previous behavior and reuse the latest rolling-out revision.
	if currentHash == "" {
		installedBundleName := ""
		if state.revisionStates.Installed != nil {
			installedBundleName = state.revisionStates.Installed.Name
		}
		SetDeprecationStatus(ext, installedBundleName, nil, false)
		state.resolvedRevisionMetadata = latestRollingOut
		return true
	}

	anyHasHash := false
	for i := len(state.revisionStates.RollingOut) - 1; i >= 0; i-- {
		rollingOut := state.revisionStates.RollingOut[i]
		if rollingOut.SourceSpecHash != "" {
			anyHasHash = true
			if rollingOut.SourceSpecHash == currentHash {
				installedBundleName := ""
				if state.revisionStates.Installed != nil {
					installedBundleName = state.revisionStates.Installed.Name
				}
				SetDeprecationStatus(ext, installedBundleName, nil, false)
				state.resolvedRevisionMetadata = rollingOut
				return true
			}
		}
	}

	// Backward compatibility: if no rolling-out revision has a hash (pre-upgrade
	// revisions), reuse the latest to avoid unnecessary catalog churn.
	if !anyHasHash {
		installedBundleName := ""
		if state.revisionStates.Installed != nil {
			installedBundleName = state.revisionStates.Installed.Name
		}
		SetDeprecationStatus(ext, installedBundleName, nil, false)
		state.resolvedRevisionMetadata = latestRollingOut
		return true
	}

	l.Info("no rolling-out revision matches current catalog spec hash, re-resolving bundle",
		"rollingOutCount", len(state.revisionStates.RollingOut),
		"currentSpecHash", currentHash,
	)
	return false
}

// versionMatchesSpec checks whether a given version string satisfies the version constraint
// in the ClusterExtension spec. Returns true if the spec has no version constraint, or if
// the version falls within the specified range. Returns false if there is a mismatch or
// if either the constraint or the version string is unparseable.
func versionMatchesSpec(version string, ext *ocv1.ClusterExtension) bool {
	if ext.Spec.Source.Catalog == nil {
		return true
	}
	specVersion := ext.Spec.Source.Catalog.Version
	if specVersion == "" {
		return true
	}
	if version == "" {
		return false
	}
	versionRange, err := compare.NewVersionRange(specVersion)
	if err != nil {
		return false
	}
	v, err := bsemver.Parse(version)
	if err != nil {
		return false
	}
	return versionRange(v)
}

// normalizeLabelSelector returns a deep copy of the selector with MatchExpressions
// and their Values sorted so that semantically equivalent selectors produce the
// same JSON serialization (and therefore the same hash).
func normalizeLabelSelector(sel *metav1.LabelSelector) *metav1.LabelSelector {
	if sel == nil {
		return nil
	}
	// A selector with no MatchLabels and no MatchExpressions is semantically
	// equivalent to nil (both match everything). Canonicalize to nil so they
	// hash identically.
	if len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) == 0 {
		return nil
	}
	n := sel.DeepCopy()
	for i := range n.MatchExpressions {
		sort.Strings(n.MatchExpressions[i].Values)
	}
	sort.Slice(n.MatchExpressions, func(i, j int) bool {
		a, b := n.MatchExpressions[i], n.MatchExpressions[j]
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		if a.Operator != b.Operator {
			return string(a.Operator) < string(b.Operator)
		}
		if len(a.Values) != len(b.Values) {
			return len(a.Values) < len(b.Values)
		}
		for k := range a.Values {
			if a.Values[k] != b.Values[k] {
				return a.Values[k] < b.Values[k]
			}
		}
		return false
	})
	return n
}

// CatalogSpecHash returns a SHA-256 hex digest of the resolution-relevant fields
// from the ClusterExtension's catalog spec. Two specs that would drive the resolver
// to evaluate different candidate sets produce different hashes.
func CatalogSpecHash(ext *ocv1.ClusterExtension) string {
	if ext.Spec.Source.Catalog == nil {
		return ""
	}
	cat := ext.Spec.Source.Catalog

	// Sort a copy of channels so that order differences don't change the hash.
	channels := slices.Clone(cat.Channels)
	sort.Strings(channels)

	data := struct {
		PackageName             string                       `json:"p"`
		Version                 string                       `json:"v,omitempty"`
		Channels                []string                     `json:"ch,omitempty"`
		Selector                *metav1.LabelSelector        `json:"s,omitempty"`
		UpgradeConstraintPolicy ocv1.UpgradeConstraintPolicy `json:"u,omitempty"`
	}{
		PackageName:             cat.PackageName,
		Version:                 cat.Version,
		Channels:                channels,
		Selector:                normalizeLabelSelector(cat.Selector),
		UpgradeConstraintPolicy: cat.UpgradeConstraintPolicy,
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// handleResolutionError handles the case when bundle resolution fails.
//
// Decision logic (evaluated in order):
//  1. No installed bundle → Retry (cannot proceed without any bundle)
//  2. Version change requested → Retry (cannot upgrade without catalog)
//  3. Cannot check catalog existence → Retry (API error, cannot safely decide)
//  4. Catalogs exist → Retry (transient error, catalog may be updating)
//  5. Catalogs deleted → Fallback to installed bundle (maintain current state)
//
// When falling back (case 5), we set the resolved bundle to the installed bundle and return
// no error, allowing the Apply step to run and maintain resources using the existing installation.
// The controller watches ClusterCatalog resources, so reconciliation will automatically resume
// when catalogs return, enabling upgrades.
func handleResolutionError(ctx context.Context, c client.Client, state *reconcileState, ext *ocv1.ClusterExtension, err error) (*ctrl.Result, error) {
	l := log.FromContext(ctx)

	// No installed bundle and resolution failed - cannot proceed
	if state.revisionStates.Installed == nil {
		msg := fmt.Sprintf("failed to resolve bundle: %v", err)
		setStatusProgressing(ext, err)
		setInstalledStatusFromRevisionStates(ext, state.revisionStates)
		ensureFailureConditionsWithReason(ext, ocv1.ReasonRetrying, msg)
		return nil, err
	}

	// Check if the spec is requesting a version that differs from what's installed.
	// Uses semver range matching so that ranges like ">=1.0.0, <2.0.0" are correctly
	// recognized as satisfied by "1.0.0".
	if !versionMatchesSpec(state.revisionStates.Installed.Version, ext) {
		specVersion := ""
		if ext.Spec.Source.Catalog != nil {
			specVersion = ext.Spec.Source.Catalog.Version
		}
		installedVersion := state.revisionStates.Installed.Version
		msg := fmt.Sprintf("unable to upgrade to version %s: %v (currently installed: %s)", specVersion, err, installedVersion)
		l.Error(err, "resolution failed and spec requests version change - cannot fall back",
			"requestedVersion", specVersion,
			"installedVersion", installedVersion)
		setStatusProgressing(ext, err)
		setInstalledStatusFromRevisionStates(ext, state.revisionStates)
		ensureFailureConditionsWithReason(ext, ocv1.ReasonRetrying, msg)
		return nil, err
	}

	// No version change requested - check if ClusterCatalogs exist
	// Only fall back if ClusterCatalogs have been deleted
	catalogsExist, catalogCheckErr := CheckCatalogsExist(ctx, c, ext)
	if catalogCheckErr != nil {
		msg := fmt.Sprintf("failed to resolve bundle: %v", err)
		var catalogName string
		if ext.Spec.Source.Catalog != nil {
			catalogName = getCatalogNameFromSelector(ext.Spec.Source.Catalog.Selector)
		}
		l.Error(catalogCheckErr, "error checking if ClusterCatalogs exist, will retry resolution",
			"resolutionError", err,
			"packageName", getPackageName(ext),
			"catalogName", catalogName)
		setStatusProgressing(ext, err)
		setInstalledStatusFromRevisionStates(ext, state.revisionStates)
		ensureFailureConditionsWithReason(ext, ocv1.ReasonRetrying, msg)
		return nil, err
	}

	if catalogsExist {
		// ClusterCatalogs exist but resolution failed - likely a transient issue (ClusterCatalog updating, cache stale, etc.)
		// Retry resolution instead of falling back
		msg := fmt.Sprintf("failed to resolve bundle, retrying: %v", err)
		var catalogName string
		if ext.Spec.Source.Catalog != nil {
			catalogName = getCatalogNameFromSelector(ext.Spec.Source.Catalog.Selector)
		}
		l.Error(err, "resolution failed but matching ClusterCatalogs exist - retrying instead of falling back",
			"packageName", getPackageName(ext),
			"catalogName", catalogName)
		setStatusProgressing(ext, err)
		setInstalledStatusFromRevisionStates(ext, state.revisionStates)
		ensureFailureConditionsWithReason(ext, ocv1.ReasonRetrying, msg)
		return nil, err
	}

	// ClusterCatalogs don't exist (deleted) - fall back to installed bundle to maintain current state.
	// The controller watches ClusterCatalog resources, so when ClusterCatalogs become available again,
	// a reconcile will be triggered automatically, allowing the extension to upgrade.
	var catalogName string
	if ext.Spec.Source.Catalog != nil {
		catalogName = getCatalogNameFromSelector(ext.Spec.Source.Catalog.Selector)
	}
	l.Info("matching ClusterCatalogs unavailable or deleted - falling back to installed bundle to maintain workload",
		"resolutionError", err.Error(),
		"packageName", getPackageName(ext),
		"catalogName", catalogName,
		"installedBundle", state.revisionStates.Installed.Name,
		"installedVersion", state.revisionStates.Installed.Version)
	// Set installed status based on current revision states (needed before Apply runs)
	setInstalledStatusFromRevisionStates(ext, state.revisionStates)
	state.resolvedRevisionMetadata = state.revisionStates.Installed
	// Return no error to allow Apply step to run and maintain resources.
	// Apply will set Progressing=Succeeded when it completes successfully.
	return nil, nil
}

// getCatalogNameFromSelector extracts the catalog name from the selector if available.
// Returns empty string if selector is nil or doesn't contain the metadata.name label.
func getCatalogNameFromSelector(selector *metav1.LabelSelector) string {
	if selector == nil || selector.MatchLabels == nil {
		return ""
	}
	return selector.MatchLabels["olm.operatorframework.io/metadata.name"]
}

// getPackageName safely extracts the package name from the extension spec.
// Returns empty string if Catalog source is nil.
func getPackageName(ext *ocv1.ClusterExtension) string {
	if ext.Spec.Source.Catalog == nil {
		return ""
	}
	return ext.Spec.Source.Catalog.PackageName
}

// CheckCatalogsExist checks if any ClusterCatalogs matching the extension's selector exist.
// Returns true if at least one matching ClusterCatalog exists, false if none exist.
// Treats "CRD doesn't exist" errors as "no ClusterCatalogs exist" (returns false, nil).
// Returns an error only if the check itself fails unexpectedly.
func CheckCatalogsExist(ctx context.Context, c client.Client, ext *ocv1.ClusterExtension) (bool, error) {
	var catalogList *ocv1.ClusterCatalogList
	var listErr error

	if ext.Spec.Source.Catalog == nil || ext.Spec.Source.Catalog.Selector == nil {
		// No selector means all ClusterCatalogs match - check if any ClusterCatalogs exist at all
		catalogList = &ocv1.ClusterCatalogList{}
		listErr = c.List(ctx, catalogList, client.Limit(1))
	} else {
		// Convert label selector to k8slabels.Selector
		// Note: An empty LabelSelector matches everything by default
		selector, err := metav1.LabelSelectorAsSelector(ext.Spec.Source.Catalog.Selector)
		if err != nil {
			return false, fmt.Errorf("invalid catalog selector: %w", err)
		}

		// List ClusterCatalogs matching the selector (limit to 1 since we only care if any exist)
		catalogList = &ocv1.ClusterCatalogList{}
		listErr = c.List(ctx, catalogList, client.MatchingLabelsSelector{Selector: selector}, client.Limit(1))
	}

	if listErr != nil {
		// Check if the error is because the ClusterCatalog CRD doesn't exist
		// This can happen if catalogd is not installed, which means no ClusterCatalogs exist
		if apimeta.IsNoMatchError(listErr) {
			return false, nil
		}
		return false, fmt.Errorf("failed to list ClusterCatalogs: %w", listErr)
	}

	return len(catalogList.Items) > 0, nil
}

func UnpackBundle(i imageutil.Puller, cache imageutil.Cache) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)

		// Defensive check: resolvedRevisionMetadata should be set by ResolveBundle step
		if state.resolvedRevisionMetadata == nil {
			return nil, fmt.Errorf("unable to retrieve bundle information")
		}

		// Always try to pull the bundle content (Pull uses cache-first strategy, so this is efficient)
		l.V(1).Info("pulling bundle content")
		imageFS, _, _, err := i.Pull(ctx, ext.GetName(), state.resolvedRevisionMetadata.Image, cache)

		// Check if resolved bundle matches installed bundle (no version change)
		bundleUnchanged := state.revisionStates != nil &&
			state.revisionStates.Installed != nil &&
			state.resolvedRevisionMetadata.Name == state.revisionStates.Installed.Name &&
			state.resolvedRevisionMetadata.Version == state.revisionStates.Installed.Version

		if err != nil {
			if bundleUnchanged {
				// Bundle hasn't changed and Pull failed (likely cache miss + catalog unavailable).
				// This happens in fallback mode after catalog deletion. Set imageFS to nil so the
				// applier can maintain the workload using existing Helm release or ClusterObjectSet.
				l.V(1).Info("bundle content unavailable but version unchanged, maintaining current installation",
					"bundle", state.resolvedRevisionMetadata.Name,
					"version", state.resolvedRevisionMetadata.Version,
					"error", err.Error())
				state.imageFS = nil
				return nil, nil
			}
			// New bundle version but Pull failed - this is an error condition
			setStatusProgressing(ext, wrapErrorWithResolutionInfo(state.resolvedRevisionMetadata.BundleMetadata, err))
			setInstalledStatusFromRevisionStates(ext, state.revisionStates)
			return nil, err
		}

		if bundleUnchanged {
			l.V(1).Info("bundle unchanged, using cached content for resource reconciliation",
				"bundle", state.resolvedRevisionMetadata.Name,
				"version", state.resolvedRevisionMetadata.Version)
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
			labels.SourceSpecHashKey:  CatalogSpecHash(ext),
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

		// After a successful rollout the installed bundle may have changed
		// (e.g. upgrade from a deprecated to a non-deprecated version).
		// Refresh deprecation conditions so they reflect the newly running
		// bundle instead of the pre-upgrade bundle that was used during
		// resolution.
		if rolloutSucceeded && state.revisionStates.Installed != nil {
			SetDeprecationStatus(ext, state.revisionStates.Installed.Name, state.resolvedDeprecation, state.hasCatalogData)
		}

		// If there was an error applying the resolved bundle,
		// report the error via the Progressing condition.
		if err != nil {
			setStatusProgressing(ext, wrapErrorWithResolutionInfo(state.resolvedRevisionMetadata.BundleMetadata, err))
			return nil, err
		} else if !rolloutSucceeded {
			apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
				Type:               ocv1.TypeProgressing,
				Status:             metav1.ConditionTrue,
				Reason:             ocv1.ReasonRollingOut,
				Message:            rolloutStatus,
				ObservedGeneration: ext.GetGeneration(),
			})
		} else {
			setStatusProgressing(ext, nil)
		}
		return nil, nil
	}
}
