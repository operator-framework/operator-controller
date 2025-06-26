/*
Copyright 2024.

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
	"testing"

	bsemver "github.com/blang/semver/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// mockResolver is a test implementation of the Resolver interface
type mockResolver struct {
	resolveFunc func(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error)
}

func (m *mockResolver) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
	return m.resolveFunc(ctx, ext, installedBundle)
}

// mockInstalledBundleGetter is a test implementation of the InstalledBundleGetter interface
type mockInstalledBundleGetter struct {
	getInstalledBundleFunc func(ctx context.Context, ext *ocv1.ClusterExtension) (*InstalledBundle, error)
}

func (m *mockInstalledBundleGetter) GetInstalledBundle(ctx context.Context, ext *ocv1.ClusterExtension) (*InstalledBundle, error) {
	return m.getInstalledBundleFunc(ctx, ext)
}

func TestIsPinnedVersion(t *testing.T) {
	testCases := []struct {
		name              string
		versionConstraint string
		installedVersion  string
		expected          bool
	}{
		{
			name:              "exact version match - pinned",
			versionConstraint: "1.2.3",
			installedVersion:  "1.2.3",
			expected:          true,
		},
		{
			name:              "exact version mismatch - not pinned",
			versionConstraint: "1.2.3",
			installedVersion:  "1.2.4",
			expected:          false,
		},
		{
			name:              "range constraint - not pinned",
			versionConstraint: ">=1.2.3",
			installedVersion:  "1.2.3",
			expected:          false,
		},
		{
			name:              "range constraint with upper bound - not pinned",
			versionConstraint: ">=1.2.3, <2.0.0",
			installedVersion:  "1.2.3",
			expected:          false,
		},
		{
			name:              "tilde constraint - not pinned",
			versionConstraint: "~1.2.3",
			installedVersion:  "1.2.3",
			expected:          false,
		},
		{
			name:              "caret constraint - not pinned",
			versionConstraint: "^1.2.3",
			installedVersion:  "1.2.3",
			expected:          false,
		},
		{
			name:              "OR constraint - not pinned",
			versionConstraint: "1.2.3 || 1.2.4",
			installedVersion:  "1.2.3",
			expected:          false,
		},
		{
			name:              "whitespace handling - pinned",
			versionConstraint: " 1.2.3 ",
			installedVersion:  " 1.2.3 ",
			expected:          true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isPinnedVersion(tc.versionConstraint, tc.installedVersion)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFindAvailableUpgrade_PinnedVersion(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))

	// Create a ClusterExtension with a pinned version
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-extension",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: "test-package",
					Version:     "1.2.3", // Pinned version
				},
			},
		},
	}

	installedBundle := &ocv1.BundleMetadata{
		Name:    "test-package.v1.2.3",
		Version: "1.2.3",
	}

	// Mock resolver - shouldn't be called for pinned versions
	mockRes := &mockResolver{
		resolveFunc: func(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
			t.Error("Resolver should not be called for pinned versions")
			return nil, nil, nil, nil
		},
	}

	mockBundleGetter := &mockInstalledBundleGetter{}

	reconciler := &ClusterExtensionRevisionReconciler{
		Client:                fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:                scheme,
		Resolver:              mockRes,
		InstalledBundleGetter: mockBundleGetter,
	}

	ctx := context.Background()
	upgrade, err := reconciler.findAvailableUpgrade(ctx, ext, installedBundle)

	require.NoError(t, err)
	assert.Nil(t, upgrade, "No upgrade should be available for pinned versions")
}

func TestFindAvailableUpgrade_VersionRange(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))

	// Create a ClusterExtension with a version range
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-extension",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: "test-package",
					Version:     ">=1.2.0, <2.0.0", // Version range
				},
			},
		},
	}

	installedBundle := &ocv1.BundleMetadata{
		Name:    "test-package.v1.2.3",
		Version: "1.2.3",
	}

	// Mock resolver to return an upgrade within the range
	expectedBundle := &declcfg.Bundle{
		Name: "test-package.v1.3.0",
	}
	expectedVersion := bsemver.MustParse("1.3.0")

	mockRes := &mockResolver{
		resolveFunc: func(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
			// Verify that the original extension (with version constraint) is passed
			assert.Equal(t, ">=1.2.0, <2.0.0", ext.Spec.Source.Catalog.Version)
			return expectedBundle, &expectedVersion, nil, nil
		},
	}

	mockBundleGetter := &mockInstalledBundleGetter{}

	reconciler := &ClusterExtensionRevisionReconciler{
		Client:                fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:                scheme,
		Resolver:              mockRes,
		InstalledBundleGetter: mockBundleGetter,
	}

	ctx := context.Background()
	upgrade, err := reconciler.findAvailableUpgrade(ctx, ext, installedBundle)

	require.NoError(t, err)
	require.NotNil(t, upgrade)
	assert.Equal(t, expectedBundle, upgrade.Bundle)
	assert.Equal(t, &expectedVersion, upgrade.Version)
}

func TestFindAvailableUpgrade_NoVersionConstraint(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))

	// Create a ClusterExtension without version constraint
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-extension",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: "test-package",
					// No version constraint
				},
			},
		},
	}

	installedBundle := &ocv1.BundleMetadata{
		Name:    "test-package.v1.2.3",
		Version: "1.2.3",
	}

	// Mock resolver to return an upgrade
	expectedBundle := &declcfg.Bundle{
		Name: "test-package.v2.0.0",
	}
	expectedVersion := bsemver.MustParse("2.0.0")

	mockRes := &mockResolver{
		resolveFunc: func(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
			// Verify that version constraint is removed
			assert.Empty(t, ext.Spec.Source.Catalog.Version)
			return expectedBundle, &expectedVersion, nil, nil
		},
	}

	mockBundleGetter := &mockInstalledBundleGetter{}

	reconciler := &ClusterExtensionRevisionReconciler{
		Client:                fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:                scheme,
		Resolver:              mockRes,
		InstalledBundleGetter: mockBundleGetter,
	}

	ctx := context.Background()
	upgrade, err := reconciler.findAvailableUpgrade(ctx, ext, installedBundle)

	require.NoError(t, err)
	require.NotNil(t, upgrade)
	assert.Equal(t, expectedBundle, upgrade.Bundle)
	assert.Equal(t, &expectedVersion, upgrade.Version)
}

func TestFindAvailableUpgrade_NoUpgradeAvailable(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))

	// Create a ClusterExtension with version range
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-extension",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: "test-package",
					Version:     ">=1.2.0, <2.0.0",
				},
			},
		},
	}

	installedBundle := &ocv1.BundleMetadata{
		Name:    "test-package.v1.5.0",
		Version: "1.5.0",
	}

	// Mock resolver to return the same version (no upgrade)
	currentBundle := &declcfg.Bundle{
		Name: "test-package.v1.5.0",
	}
	currentVersion := bsemver.MustParse("1.5.0")

	mockRes := &mockResolver{
		resolveFunc: func(ctx context.Context, ext *ocv1.ClusterExtension, installedBundle *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
			return currentBundle, &currentVersion, nil, nil
		},
	}

	mockBundleGetter := &mockInstalledBundleGetter{}

	reconciler := &ClusterExtensionRevisionReconciler{
		Client:                fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:                scheme,
		Resolver:              mockRes,
		InstalledBundleGetter: mockBundleGetter,
	}

	ctx := context.Background()
	upgrade, err := reconciler.findAvailableUpgrade(ctx, ext, installedBundle)

	require.NoError(t, err)
	assert.Nil(t, upgrade, "No upgrade should be available when resolved version is not newer")
}
