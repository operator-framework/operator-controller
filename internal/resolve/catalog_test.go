package resolve

import (
	"context"
	"fmt"
	"testing"

	bsemver "github.com/blang/semver/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/pkg/features"
)

func TestInvalidClusterExtensionVersionRange(t *testing.T) {
	r := CatalogResolver{}
	pkgName := randPkg()
	ce := buildFooClusterExtension(pkgName, "", "foobar", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, `desired version range "foobar" is invalid: improper constraint: foobar`)
}

func TestErrorWalkingCatalogs(t *testing.T) {
	r := CatalogResolver{WalkCatalogsFunc: func(context.Context, string, CatalogWalkFunc, ...client.ListOption) error {
		return fmt.Errorf("fake error")
	}}
	pkgName := randPkg()
	ce := buildFooClusterExtension(pkgName, "", "", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, "error walking catalogs: fake error")
}

func TestErrorGettingPackage(t *testing.T) {
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return nil, fmt.Errorf("fake error") },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	pkgName := randPkg()
	ce := buildFooClusterExtension(pkgName, "", "", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, fmt.Sprintf(`error walking catalogs: error getting package %q from catalog "a": fake error`, pkgName))
}

func TestPackageDoesNotExist(t *testing.T) {
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	pkgName := randPkg()
	ce := buildFooClusterExtension(pkgName, "", "", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, fmt.Sprintf(`no package %q found`, pkgName))
}

func TestPackageExists(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", "", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "2.0.0"), *gotBundle)
	assert.Equal(t, bsemver.MustParse("2.0.0"), *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestVersionDoesNotExist(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", "3.0.0", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, fmt.Sprintf(`no package %q matching version "3.0.0" found`, pkgName))
}

func TestVersionExists(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", ">=1.0.0 <2.0.0", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "1.0.2"), *gotBundle)
	assert.Equal(t, bsemver.MustParse("1.0.2"), *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestChannelDoesNotExist(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "stable", "", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, fmt.Sprintf(`no package %q in channel "stable" found`, pkgName))
}

func TestChannelExists(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "beta", "", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "1.0.2"), *gotBundle)
	assert.Equal(t, bsemver.MustParse("1.0.2"), *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestChannelExistsButNotVersion(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "beta", "3.0.0", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, fmt.Sprintf(`no package %q matching version "3.0.0" in channel "beta" found`, pkgName))
}

func TestVersionExistsButNotChannel(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "stable", "1.0.0", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, fmt.Sprintf(`no package %q matching version "1.0.0" in channel "stable" found`, pkgName))
}

func TestChannelAndVersionExist(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "alpha", "0.1.0", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "0.1.0"), *gotBundle)
	assert.Equal(t, bsemver.MustParse("0.1.0"), *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestPreferNonDeprecated(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", ">=0.1.0 <=1.0.0", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "0.1.0"), *gotBundle)
	assert.Equal(t, bsemver.MustParse("0.1.0"), *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestAcceptDeprecated(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", ">=1.0.0 <=1.0.1", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "1.0.1"), *gotBundle)
	assert.Equal(t, bsemver.MustParse("1.0.1"), *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestPackageVariationsBetweenCatalogs(t *testing.T) {
	pkgName := randPkg()
	genImgRef := func(catalog, name string) string {
		return fmt.Sprintf("%s/%s", catalog, name)
	}
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) {
			fbc := genPackage(pkgName)
			fbc.Bundles = append(fbc.Bundles, genBundle(pkgName, "1.0.3"))
			for i := range fbc.Bundles {
				fbc.Bundles[i].Image = genImgRef("catalog-b", fbc.Bundles[i].Name)
			}
			return fbc, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, error) {
			fbc := genPackage(pkgName)
			fbc.Bundles = append(fbc.Bundles, genBundle(pkgName, "0.1.1"))
			fbc.Deprecations = nil
			for i := range fbc.Bundles {
				fbc.Bundles[i].Image = genImgRef("catalog-c", fbc.Bundles[i].Name)
			}
			return fbc, nil
		},
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}

	t.Run("always prefer non-deprecated when versions match", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			// When the same version exists in both catalogs, we prefer the non-deprecated one.
			ce := buildFooClusterExtension(pkgName, "", ">=1.0.0 <=1.0.1", ocv1alpha1.UpgradeConstraintPolicyEnforce)
			gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
			require.NoError(t, err)
			assert.Equal(t, genBundle(pkgName, "1.0.1").Name, gotBundle.Name)
			assert.Equal(t, bsemver.MustParse("1.0.1"), *gotVersion)
			assert.Nil(t, gotDeprecation)
		}
	})

	t.Run("when catalog b has a newer version that matches the range", func(t *testing.T) {
		// When one version exists in one catalog but not the other, we prefer the one that exists.
		ce := buildFooClusterExtension(pkgName, "", ">=1.0.0 <=1.0.3", ocv1alpha1.UpgradeConstraintPolicyEnforce)
		gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
		require.NoError(t, err)
		assert.Equal(t, genBundle(pkgName, "1.0.3").Name, gotBundle.Name)
		assert.Equal(t, genImgRef("catalog-b", gotBundle.Name), gotBundle.Image)
		assert.Equal(t, bsemver.MustParse("1.0.3"), *gotVersion)
		assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
	})

	t.Run("when catalog c has a newer version that matches the range", func(t *testing.T) {
		ce := buildFooClusterExtension(pkgName, "", ">=0.1.0 <1.0.0", ocv1alpha1.UpgradeConstraintPolicyEnforce)
		gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
		require.NoError(t, err)
		assert.Equal(t, genBundle(pkgName, "0.1.1").Name, gotBundle.Name)
		assert.Equal(t, genImgRef("catalog-c", gotBundle.Name), gotBundle.Image)
		assert.Equal(t, bsemver.MustParse("0.1.1"), *gotVersion)
		assert.Nil(t, gotDeprecation)
	})

	t.Run("when there is ambiguity between catalogs", func(t *testing.T) {
		// When there is no way to disambiguate between two versions, the choice is undefined.
		foundImages := sets.New[string]()
		foundDeprecations := sets.New[*declcfg.Deprecation]()
		for i := 0; i < 100; i++ {
			ce := buildFooClusterExtension(pkgName, "", "0.1.0", ocv1alpha1.UpgradeConstraintPolicyEnforce)
			gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
			require.NoError(t, err)
			assert.Equal(t, genBundle(pkgName, "0.1.0").Name, gotBundle.Name)
			assert.Equal(t, bsemver.MustParse("0.1.0"), *gotVersion)
			foundImages.Insert(gotBundle.Image)
			foundDeprecations.Insert(gotDeprecation)
		}
		assert.ElementsMatch(t, []string{
			genImgRef("catalog-b", bundleName(pkgName, "0.1.0")),
			genImgRef("catalog-c", bundleName(pkgName, "0.1.0")),
		}, foundImages.UnsortedList())

		assert.Contains(t, foundDeprecations, (*declcfg.Deprecation)(nil))
		assert.Contains(t, foundDeprecations, ptr.To(packageDeprecation(pkgName)))
	})
}

func TestUpgradeFoundLegacy(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, false)()
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", "", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	installedBundle := &ocv1alpha1.BundleMetadata{
		Name:    bundleName(pkgName, "0.1.0"),
		Version: "0.1.0",
	}
	// 0.1.0 => 1.0.2 would not be allowed using semver semantics
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, installedBundle)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "1.0.2"), *gotBundle)
	assert.Equal(t, bsemver.MustParse("1.0.2"), *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestUpgradeNotFoundLegacy(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, false)()
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", "<1.0.0 >=2.0.0", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	installedBundle := &ocv1alpha1.BundleMetadata{
		Name:    bundleName(pkgName, "0.1.0"),
		Version: "0.1.0",
	}
	// 0.1.0 only upgrades to 1.0.x with its legacy upgrade edges, so this fails.
	_, _, _, err := r.Resolve(context.Background(), ce, installedBundle)
	assert.EqualError(t, err, fmt.Sprintf(`error upgrading from currently installed version "0.1.0": no package %q matching version "<1.0.0 >=2.0.0" found`, pkgName))
}

func TestUpgradeFoundSemver(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, true)()
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", "", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	installedBundle := &ocv1alpha1.BundleMetadata{
		Name:    bundleName(pkgName, "1.0.0"),
		Version: "1.0.0",
	}
	// there is a legacy upgrade edge from 1.0.0 to 2.0.0, but we are using semver semantics here.
	// therefore:
	// 	 1.0.0 => 1.0.2 is what we expect
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, installedBundle)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "1.0.2"), *gotBundle)
	assert.Equal(t, bsemver.MustParse("1.0.2"), *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}
func TestUpgradeNotFoundSemver(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, true)()
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", "!=0.1.0", ocv1alpha1.UpgradeConstraintPolicyEnforce)
	installedBundle := &ocv1alpha1.BundleMetadata{
		Name:    bundleName(pkgName, "0.1.0"),
		Version: "0.1.0",
	}
	// there are legacy upgrade edges from 0.1.0 to 1.0.x, but we are using semver semantics here.
	// therefore, we expect to fail because there are no semver-compatible upgrade edges from 0.1.0.
	_, _, _, err := r.Resolve(context.Background(), ce, installedBundle)
	assert.EqualError(t, err, fmt.Sprintf(`error upgrading from currently installed version "0.1.0": no package %q matching version "!=0.1.0" found`, pkgName))
}

func TestDowngradeFound(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", "<1.0.2", ocv1alpha1.UpgradeConstraintPolicyIgnore)
	installedBundle := &ocv1alpha1.BundleMetadata{
		Name:    bundleName(pkgName, "1.0.2"),
		Version: "1.0.2",
	}
	// 1.0.2 => 0.1.0 is a downgrade, but it is allowed because of the upgrade constraint policy.
	//   note: we chose 0.1.0 because 1.0.0 and 1.0.1 are deprecated.
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, installedBundle)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "0.1.0"), *gotBundle)
	assert.Equal(t, bsemver.MustParse("0.1.0"), *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestDowngradeNotFound(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogWalker{
		"a": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"b": func() (*declcfg.DeclarativeConfig, error) { return &declcfg.DeclarativeConfig{}, nil },
		"c": func() (*declcfg.DeclarativeConfig, error) { return genPackage(pkgName), nil },
	}
	r := CatalogResolver{WalkCatalogsFunc: w.WalkCatalogs}
	ce := buildFooClusterExtension(pkgName, "", ">0.1.0 <1.0.0", ocv1alpha1.UpgradeConstraintPolicyIgnore)
	installedBundle := &ocv1alpha1.BundleMetadata{
		Name:    bundleName(pkgName, "1.0.2"),
		Version: "1.0.2",
	}
	// Downgrades are allowed via the upgrade constraint policy, but there is no bundle in the specified range.
	_, _, _, err := r.Resolve(context.Background(), ce, installedBundle)
	assert.EqualError(t, err, fmt.Sprintf(`error upgrading from currently installed version "1.0.2": no package %q matching version ">0.1.0 <1.0.0" found`, pkgName))
}

func TestCatalogWalker(t *testing.T) {
	t.Run("error listing catalogs", func(t *testing.T) {
		w := CatalogWalker(
			func(ctx context.Context, option ...client.ListOption) ([]catalogd.ClusterCatalog, error) {
				return nil, fmt.Errorf("fake error")
			},
			func(context.Context, *catalogd.ClusterCatalog, string) (*declcfg.DeclarativeConfig, error) {
				return nil, nil
			},
		)
		walkFunc := func(ctx context.Context, cat *catalogd.ClusterCatalog, fbc *declcfg.DeclarativeConfig, err error) error {
			return nil
		}
		assert.EqualError(t, w(context.Background(), "", walkFunc), "error listing catalogs: fake error")
	})

	t.Run("error getting package", func(t *testing.T) {
		w := CatalogWalker(
			func(ctx context.Context, option ...client.ListOption) ([]catalogd.ClusterCatalog, error) {
				return []catalogd.ClusterCatalog{{ObjectMeta: metav1.ObjectMeta{Name: "a"}}}, nil
			},
			func(context.Context, *catalogd.ClusterCatalog, string) (*declcfg.DeclarativeConfig, error) {
				return nil, fmt.Errorf("fake error getting package FBC")
			},
		)
		walkFunc := func(ctx context.Context, cat *catalogd.ClusterCatalog, fbc *declcfg.DeclarativeConfig, err error) error {
			return err
		}
		assert.EqualError(t, w(context.Background(), "", walkFunc), "fake error getting package FBC")
	})

	t.Run("success", func(t *testing.T) {
		w := CatalogWalker(
			func(ctx context.Context, option ...client.ListOption) ([]catalogd.ClusterCatalog, error) {
				return []catalogd.ClusterCatalog{
					{ObjectMeta: metav1.ObjectMeta{Name: "a"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "b"}},
				}, nil
			},
			func(context.Context, *catalogd.ClusterCatalog, string) (*declcfg.DeclarativeConfig, error) {
				return &declcfg.DeclarativeConfig{}, nil
			},
		)

		seenCatalogs := []string{}
		walkFunc := func(ctx context.Context, cat *catalogd.ClusterCatalog, fbc *declcfg.DeclarativeConfig, err error) error {
			seenCatalogs = append(seenCatalogs, cat.Name)
			return nil
		}
		assert.NoError(t, w(context.Background(), "", walkFunc))
		assert.Equal(t, []string{"a", "b"}, seenCatalogs)
	})
}

func buildFooClusterExtension(pkg, channel, version string, upgradeConstraintPolicy ocv1alpha1.UpgradeConstraintPolicy) *ocv1alpha1.ClusterExtension {
	return &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: pkg,
		},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			InstallNamespace:        "default",
			ServiceAccount:          ocv1alpha1.ServiceAccountReference{Name: "default"},
			PackageName:             pkg,
			Channel:                 channel,
			Version:                 version,
			UpgradeConstraintPolicy: upgradeConstraintPolicy,
		},
	}
}

type getPackageFunc func() (*declcfg.DeclarativeConfig, error)

type staticCatalogWalker map[string]getPackageFunc

func (w staticCatalogWalker) WalkCatalogs(ctx context.Context, _ string, f CatalogWalkFunc, _ ...client.ListOption) error {
	for k, v := range w {
		cat := &catalogd.ClusterCatalog{
			ObjectMeta: metav1.ObjectMeta{Name: k},
		}
		fbc, fbcErr := v()
		if err := f(ctx, cat, fbc, fbcErr); err != nil {
			return err
		}
	}
	return nil
}

func randPkg() string {
	return fmt.Sprintf("pkg-%s", rand.String(5))
}

func bundleName(pkg, version string) string {
	return fmt.Sprintf("%s.v%s", pkg, version)
}

func genBundle(pkg, version string) declcfg.Bundle {
	return declcfg.Bundle{
		Package: pkg,
		Name:    bundleName(pkg, version),
		Properties: []property.Property{
			property.MustBuildPackage(pkg, version),
		},
	}
}

func packageDeprecation(pkg string) declcfg.Deprecation {
	return declcfg.Deprecation{
		Package: pkg,
		Entries: []declcfg.DeprecationEntry{
			{
				Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaBundle, Name: bundleName(pkg, "1.0.0")},
				Message:   fmt.Sprintf("bundle %s is deprecated", bundleName(pkg, "1.0.0")),
			},
			{
				Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaBundle, Name: bundleName(pkg, "1.0.1")},
				Message:   fmt.Sprintf("bundle %s is deprecated", bundleName(pkg, "1.0.1")),
			},
		},
	}
}

func genPackage(pkg string) *declcfg.DeclarativeConfig {
	return &declcfg.DeclarativeConfig{
		Packages: []declcfg.Package{{Name: pkg}},
		Channels: []declcfg.Channel{
			{Package: pkg, Name: "alpha", Entries: []declcfg.ChannelEntry{
				{Name: bundleName(pkg, "0.1.0")},
				{Name: bundleName(pkg, "1.0.0"), SkipRange: "<1.0.0"},
				{Name: bundleName(pkg, "1.0.1"), SkipRange: "<1.0.1"},
				{Name: bundleName(pkg, "1.0.2"), SkipRange: "<1.0.2"},
				{Name: bundleName(pkg, "2.0.0"), SkipRange: ">=1.0.0 <2.0.0"},
			}},
			{Package: pkg, Name: "beta", Entries: []declcfg.ChannelEntry{
				{Name: bundleName(pkg, "0.1.0")},
				{Name: bundleName(pkg, "1.0.2"), SkipRange: "<1.0.2"},
			}},
		},
		Bundles: []declcfg.Bundle{
			genBundle(pkg, "0.1.0"),
			genBundle(pkg, "1.0.0"),
			genBundle(pkg, "1.0.1"),
			genBundle(pkg, "1.0.2"),
			genBundle(pkg, "2.0.0"),
		},
		Deprecations: []declcfg.Deprecation{packageDeprecation(pkg)},
	}
}
