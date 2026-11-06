/*
Copyright 2026.

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
	"fmt"
	"reflect"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// Resolver is the mandatory behavior of an object that can resolve a bundle
// from a source. The shared reconciliation steps own engagement policy:
// ValidateSource runs before rollout continuation and resolution, Resolve is
// called only at steady state with no rollout in progress,
// RecoverFromResolutionError is invoked only when Resolve fails, and
// CanMaintainWithoutContent is consulted only when bundle content cannot be
// pulled. Implementations must not hide preconditions or fallbacks inside
// Resolve.
//
// Deprecation evidence is deliberately not part of this contract: sources
// that can provide it implement the optional deprecation interfaces in the
// resolve package (see resolve.PackageDeprecationSource), and consumers use
// type assertions against those interfaces to collect evidence on every
// reconcile. A source without deprecation data is reported exactly like a
// source whose evaluation found no deprecations.
type Resolver interface {
	// ValidateSource verifies the extension's source payload is complete
	// enough to reconcile. It runs before rollout continuation and
	// resolution, so malformed payloads fail closed without backend calls.
	ValidateSource(ext *ocv1.ClusterExtension) error

	// Resolve selects the bundle to install or upgrade to. It is called only
	// at steady state, with no rollout in progress.
	Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*RevisionMetadata, error)

	// RecoverFromResolutionError is invoked only when Resolve returned an
	// error. Returning a nil error with a non-nil result authorizes
	// maintaining the installed bundle (see maintainInstalled); returning an
	// error classifies the failure for retry.
	RecoverFromResolutionError(ctx context.Context, ext *ocv1.ClusterExtension, revisions *RevisionStates, resolveErr error) (*RevisionMetadata, error)

	// CanMaintainWithoutContent authorizes maintaining the current
	// installation when bundle content cannot be pulled.
	CanMaintainWithoutContent(ext *ocv1.ClusterExtension, revisions *RevisionStates, resolved *RevisionMetadata) bool

	// ReconcileOnCatalogChange reports whether a ClusterCatalog event should
	// enqueue reconciles for extensions served by this source.
	ReconcileOnCatalogChange(ext *ocv1.ClusterExtension, catalog *ocv1.ClusterCatalog) bool
}

// Resolvers selects a source policy using spec.source.sourceType.
type Resolvers map[string]Resolver

// For returns the configured policy or an error before any source is invoked.
func (sources Resolvers) For(ext *ocv1.ClusterExtension) (Resolver, error) {
	source := sources[ext.Spec.Source.SourceType]
	if source == nil || nilResolver(source) {
		return nil, fmt.Errorf("unsupported sourceType %q: bundle source is not configured", ext.Spec.Source.SourceType)
	}
	return source, nil
}

// maintainInstalled is the general installed-bundle fallback action: it
// targets the installed revision so the shared unpack and apply pipeline
// maintains the existing installation.
func maintainInstalled(revisions *RevisionStates) *RevisionMetadata {
	return revisions.Installed
}

// Interfaces can contain typed nil implementations; reject these at lookup too.
func nilResolver(source Resolver) bool {
	v := reflect.ValueOf(source)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
