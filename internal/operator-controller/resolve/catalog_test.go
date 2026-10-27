package resolve

import (
	"context"
	"errors"
	"fmt"
	"testing"

	bsemver "github.com/blang/semver/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestInvalidClusterExtensionVersionRange(t *testing.T) {
	r := CatalogResolver{}
	pkgName := randPkg()
	ce := buildFooClusterExtension(pkgName, []string{}, "foobar", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, `desired version range "foobar" is invalid: improper constraint: "foobar"`)
}

func TestErrorWalkingCatalogs(t *testing.T) {
	r := CatalogResolver{ListCatalogsFunc: func(context.Context, ...client.ListOption) ([]ocv1.ClusterCatalog, error) {
		return nil, fmt.Errorf("fake error")
	}}
	pkgName := randPkg()
	ce := buildFooClusterExtension(pkgName, []string{}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, "error listing catalogs: fake error")
}

func TestErrorGettingPackage(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), &ocv1.ClusterCatalogSpec{Priority: 1}, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return nil, &ocv1.ClusterCatalogSpec{Priority: 1}, fmt.Errorf("fake error")
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), &ocv1.ClusterCatalogSpec{Priority: 0}, nil
		},
	}
	var consulted []string
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: func(ctx context.Context, cat *ocv1.ClusterCatalog, name string) (*declcfg.DeclarativeConfig, error) {
		consulted = append(consulted, cat.Name)
		return w.GetPackage(ctx, cat, name)
	}}
	ce := buildFooClusterExtension(pkgName, []string{}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, _, deprecation, err := r.Resolve(context.Background(), ce, nil)
	require.EqualError(t, err, fmt.Sprintf(`error getting package %q from catalog "b": fake error`, pkgName))
	assert.Equal(t, []string{"a", "b"}, consulted)
	assert.Nil(t, deprecation)
}

func TestPackageDoesNotExist(t *testing.T) {
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	pkgName := randPkg()
	ce := buildFooClusterExtension(pkgName, []string{}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, fmt.Sprintf(`package %q not found in any eligible catalog`, pkgName))
}

func TestPackageExists(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "3.0.0"), *gotBundle)
	assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("3.0.0")}, *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestValidationFailed(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), &ocv1.ClusterCatalogSpec{Priority: 0}, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), &ocv1.ClusterCatalogSpec{Priority: 1}, nil
		},
	}
	var consulted []string
	r := CatalogResolver{
		ListCatalogsFunc: w.ListCatalogs,
		GetPackageFunc: func(ctx context.Context, cat *ocv1.ClusterCatalog, name string) (*declcfg.DeclarativeConfig, error) {
			consulted = append(consulted, cat.Name)
			return w.GetPackage(ctx, cat, name)
		},
		Validations: []ValidationFunc{
			func(b *declcfg.Bundle) error {
				return errors.New("fail")
			},
		},
	}
	ce := buildFooClusterExtension(pkgName, []string{}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, _, deprecation, err := r.Resolve(context.Background(), ce, nil)
	require.ErrorContains(t, err, `from catalog "c": validating bundle`)
	assert.Equal(t, []string{"c"}, consulted)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), deprecation)
}

func TestVersionDoesNotExist(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, "4.0.0", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, _, deprecation, err := r.Resolve(context.Background(), ce, nil)
	require.EqualError(t, err, fmt.Sprintf(`resolving package %q from catalog "c": no bundles found for package %q matching version "4.0.0"`, pkgName, pkgName))
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), deprecation)
}

func TestVersionExists(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, ">=1.0.0 <2.0.0", ocv1.UpgradeConstraintPolicyCatalogProvided)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "1.0.2"), *gotBundle)
	assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("1.0.2")}, *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestChannelDoesNotExist(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{"stable"}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, fmt.Sprintf(`resolving package %q from catalog "c": no bundles found for package %q in channels [stable]`, pkgName, pkgName))
}

func TestChannelExists(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{"beta"}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "1.0.2"), *gotBundle)
	assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("1.0.2")}, *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestChannelExistsButNotVersion(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{"beta"}, "3.0.0", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, fmt.Sprintf(`resolving package %q from catalog "c": no bundles found for package %q matching version "3.0.0" in channels [beta]`, pkgName, pkgName))
}

func TestVersionExistsButNotChannel(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{"stable"}, "1.0.0", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, fmt.Sprintf(`resolving package %q from catalog "c": no bundles found for package %q matching version "1.0.0" in channels [stable]`, pkgName, pkgName))
}

func TestChannelAndVersionExist(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{"alpha"}, "0.1.0", ocv1.UpgradeConstraintPolicyCatalogProvided)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "0.1.0"), *gotBundle)
	assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("0.1.0")}, *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestPreferNonDeprecated(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, ">=0.1.0 <=1.0.0", ocv1.UpgradeConstraintPolicyCatalogProvided)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "0.1.0"), *gotBundle)
	assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("0.1.0")}, *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestAcceptDeprecated(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, ">=1.0.0 <=1.0.1", ocv1.UpgradeConstraintPolicyCatalogProvided)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "1.0.1"), *gotBundle)
	assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("1.0.1")}, *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestPackageVariationsBetweenCatalogs(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			fbc := &declcfg.DeclarativeConfig{
				Packages: []declcfg.Package{{Name: pkgName}},
				Bundles:  []declcfg.Bundle{genBundle(pkgName, "1.0.0")},
				Deprecations: []declcfg.Deprecation{{
					Package: pkgName,
					Entries: []declcfg.DeprecationEntry{
						{
							Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaBundle, Name: bundleName(pkgName, "1.0.0")},
							Message:   fmt.Sprintf("bundle %s is deprecated", bundleName(pkgName, "1.0.0")),
						},
					},
				}},
			}
			return fbc, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			fbc := &declcfg.DeclarativeConfig{
				Packages: []declcfg.Package{{Name: pkgName}},
				Bundles:  []declcfg.Bundle{genBundle(pkgName, "1.0.1")},
				Deprecations: []declcfg.Deprecation{{
					Package: pkgName,
					Entries: []declcfg.DeprecationEntry{
						{
							Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaBundle, Name: bundleName(pkgName, "1.0.1")},
							Message:   fmt.Sprintf("bundle %s is deprecated", bundleName(pkgName, "1.0.1")),
						},
					},
				}},
			}
			return fbc, nil, nil
		},
		"d": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			fbc := &declcfg.DeclarativeConfig{
				Packages: []declcfg.Package{{Name: pkgName}},
				Bundles:  []declcfg.Bundle{genBundle(pkgName, "1.0.2")},
			}
			return fbc, nil, nil
		},
		"e": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			fbc := &declcfg.DeclarativeConfig{
				Packages: []declcfg.Package{{Name: pkgName}},
				Bundles:  []declcfg.Bundle{genBundle(pkgName, "1.0.3")},
				Deprecations: []declcfg.Deprecation{{
					Package: pkgName,
					Entries: []declcfg.DeprecationEntry{
						{
							Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaBundle, Name: bundleName(pkgName, "1.0.3")},
							Message:   fmt.Sprintf("bundle %s is deprecated", bundleName(pkgName, "1.0.3")),
						},
					},
				}},
			}
			return fbc, nil, nil
		},
		"f": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			fbc := &declcfg.DeclarativeConfig{
				Packages: []declcfg.Package{{Name: pkgName}},
				Bundles: []declcfg.Bundle{
					genBundle(pkgName, "1.0.4"),
					genBundle(pkgName, "1.0.5"),
				},
			}
			return fbc, nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}

	t.Run("same-priority packages are ambiguous despite non-deprecated candidates", func(t *testing.T) {
		ce := buildFooClusterExtension(pkgName, []string{}, ">=1.0.0 <=1.0.3", ocv1.UpgradeConstraintPolicyCatalogProvided)
		gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
		require.ErrorContains(t, err, "in multiple catalogs with the same priority 0: [b c d e f]")
		assert.Nil(t, gotBundle)
		assert.Nil(t, gotVersion)
		assert.Nil(t, gotDeprecation)
	})

	t.Run("highest semver bundle is chosen when candidates are all from the same catalog", func(t *testing.T) {
		ce := buildFooClusterExtension(pkgName, []string{}, ">=1.0.4 <=1.0.5", ocv1.UpgradeConstraintPolicyCatalogProvided)
		ce.Spec.Source.Catalog.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": "f"}}
		gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
		require.NoError(t, err)
		// Bundles within one catalog for a package will be sorted by semver and deprecation and the best is returned
		assert.Equal(t, genBundle(pkgName, "1.0.5").Name, gotBundle.Name)
		assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("1.0.5")}, *gotVersion)
		assert.Equal(t, &declcfg.Deprecation{Schema: declcfg.SchemaDeprecation, Package: pkgName}, gotDeprecation)
	})
}

func TestUpgradeFoundLegacy(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	installedBundle := &ocv1.BundleMetadata{
		Name:    bundleName(pkgName, "0.1.0"),
		Version: "0.1.0",
	}
	// 0.1.0 => 1.0.2 would not be allowed using semver semantics
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, installedBundle)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "1.0.2"), *gotBundle)
	assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("1.0.2")}, *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestUpgradeNotFoundLegacy(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, "<1.0.0 >=2.0.0", ocv1.UpgradeConstraintPolicyCatalogProvided)
	installedBundle := &ocv1.BundleMetadata{
		Name:    bundleName(pkgName, "0.1.0"),
		Version: "0.1.0",
	}
	// 0.1.0 only upgrades to 1.0.x with its legacy upgrade edges, so this fails.
	_, _, _, err := r.Resolve(context.Background(), ce, installedBundle)
	assert.EqualError(t, err, fmt.Sprintf(`resolving package %q from catalog "c": error upgrading from currently installed version "0.1.0": no bundles found for package %q matching version "<1.0.0 >=2.0.0"`, pkgName, pkgName))
}

func TestDowngradeFound(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, "<1.0.2", ocv1.UpgradeConstraintPolicySelfCertified)
	installedBundle := &ocv1.BundleMetadata{
		Name:    bundleName(pkgName, "1.0.2"),
		Version: "1.0.2",
	}
	// 1.0.2 => 0.1.0 is a downgrade, but it is allowed because of the upgrade constraint policy.
	//   note: we chose 0.1.0 because 1.0.0 and 1.0.1 are deprecated.
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, installedBundle)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "0.1.0"), *gotBundle)
	assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("0.1.0")}, *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestDowngradeNotFound(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, ">0.1.0 <1.0.0", ocv1.UpgradeConstraintPolicySelfCertified)
	installedBundle := &ocv1.BundleMetadata{
		Name:    bundleName(pkgName, "1.0.2"),
		Version: "1.0.2",
	}
	// Downgrades are allowed via the upgrade constraint policy, but there is no bundle in the specified range.
	_, _, _, err := r.Resolve(context.Background(), ce, installedBundle)
	assert.EqualError(t, err, fmt.Sprintf(`resolving package %q from catalog "c": error upgrading from currently installed version "1.0.2": no bundles found for package %q matching version ">0.1.0 <1.0.0"`, pkgName, pkgName))
}

func buildFooClusterExtension(pkg string, channels []string, version string, upgradeConstraintPolicy ocv1.UpgradeConstraintPolicy) *ocv1.ClusterExtension {
	return &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: pkg,
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "default",
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName:             pkg,
					Version:                 version,
					Channels:                channels,
					UpgradeConstraintPolicy: upgradeConstraintPolicy,
				},
			},
		},
	}
}

type getPackageFunc func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error)

type staticCatalogs map[string]getPackageFunc

func (w staticCatalogs) ListCatalogs(_ context.Context, opts ...client.ListOption) ([]ocv1.ClusterCatalog, error) {
	var catalogs []ocv1.ClusterCatalog
	for k, v := range w {
		cat := ocv1.ClusterCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name: k,
				Labels: map[string]string{
					"olm.operatorframework.io/metadata.name": k,
				},
			},
		}
		options := client.ListOptions{}
		for _, opt := range opts {
			opt.ApplyToList(&options)
		}
		if !options.LabelSelector.Matches(labels.Set(cat.Labels)) {
			continue
		}
		_, catCfg, _ := v()
		if catCfg != nil {
			catCfg.DeepCopyInto(&cat.Spec)
		}
		catalogs = append(catalogs, cat)
	}
	return catalogs, nil
}

func (w staticCatalogs) GetPackage(_ context.Context, cat *ocv1.ClusterCatalog, _ string) (*declcfg.DeclarativeConfig, error) {
	fbc, _, err := w[cat.Name]()
	return fbc, err
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
			{Package: pkg, Name: "gamma", Entries: []declcfg.ChannelEntry{
				{Name: bundleName(pkg, "3.0.0"), SkipRange: ">=2.0.0 <3.0.0"},
			}},
		},
		Bundles: []declcfg.Bundle{
			genBundle(pkg, "0.1.0"),
			genBundle(pkg, "1.0.0"),
			genBundle(pkg, "1.0.1"),
			genBundle(pkg, "1.0.2"),
			genBundle(pkg, "2.0.0"),
			genBundle(pkg, "3.0.0"),
		},
		Deprecations: []declcfg.Deprecation{packageDeprecation(pkg)},
	}
}

func TestInvalidClusterExtensionCatalogMatchExpressions(t *testing.T) {
	r := CatalogResolver{}
	ce := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				Catalog: &ocv1.CatalogFilter{
					PackageName: "foo",
					Selector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "name",
								Operator: metav1.LabelSelectorOperator("bad"),
								Values:   []string{"value"},
							},
						},
					},
				},
			},
		},
	}
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.EqualError(t, err, "desired catalog selector is invalid: \"bad\" is not a valid label selector operator")
}

func TestInvalidClusterExtensionCatalogMatchLabelsName(t *testing.T) {
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage("foo"), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				Catalog: &ocv1.CatalogFilter{
					PackageName: "foo",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"": "value"},
					},
				},
			},
		},
	}
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.ErrorContains(t, err, "desired catalog selector is invalid: key: Invalid value:")
}

func TestInvalidClusterExtensionCatalogMatchLabelsValue(t *testing.T) {
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage("foo"), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				Catalog: &ocv1.CatalogFilter{
					PackageName: "foo",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"name": "&value"},
					},
				},
			},
		},
	}
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	assert.ErrorContains(t, err, "desired catalog selector is invalid: values[0][name]: Invalid value:")
}

func TestClusterExtensionMatchLabel(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	ce.Spec.Source.Catalog.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": "b"},
	}

	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
}

func TestClusterExtensionNoMatchLabel(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	ce.Spec.Source.Catalog.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"olm.operatorframework.io/metadata.name": "a"},
	}

	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, fmt.Sprintf("package %q not found in any eligible catalog", pkgName))
}

func TestUnequalPriority(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{
				Packages: []declcfg.Package{{Name: pkgName}},
				Channels: []declcfg.Channel{
					{Package: pkgName, Name: "alpha", Entries: []declcfg.ChannelEntry{
						{Name: bundleName(pkgName, "1.0.0")},
					}},
				},
				Bundles: []declcfg.Bundle{
					genBundle(pkgName, "1.0.0"),
				},
				Deprecations: []declcfg.Deprecation{},
			}, &ocv1.ClusterCatalogSpec{Priority: 1}, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{
				Packages: []declcfg.Package{{Name: pkgName}},
				Channels: []declcfg.Channel{
					{Package: pkgName, Name: "alpha", Entries: []declcfg.ChannelEntry{
						{Name: bundleName(pkgName, "1.1.0")},
					}},
				},
				Bundles: []declcfg.Bundle{
					genBundle(pkgName, "1.1.0"),
				},
				Deprecations: []declcfg.Deprecation{},
			}, &ocv1.ClusterCatalogSpec{Priority: 0}, nil
		},
	}
	var consulted []string
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: func(ctx context.Context, cat *ocv1.ClusterCatalog, name string) (*declcfg.DeclarativeConfig, error) {
		consulted = append(consulted, cat.Name)
		return w.GetPackage(ctx, cat, name)
	}}

	ce := buildFooClusterExtension(pkgName, []string{}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, gotVersion, deprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	require.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("1.0.0")}, *gotVersion)
	assert.Equal(t, []string{"a"}, consulted)
	assert.Equal(t, &declcfg.Deprecation{Schema: declcfg.SchemaDeprecation, Package: pkgName}, deprecation)

	// The lower-priority catalog has 1.1.0, but the selected catalog cannot satisfy it.
	consulted = nil
	ce.Spec.Source.Catalog.Version = "1.1.0"
	_, _, deprecation, err = r.Resolve(context.Background(), ce, nil)
	require.ErrorContains(t, err, `from catalog "a": no bundles found`)
	assert.Equal(t, []string{"a"}, consulted)
	assert.Equal(t, &declcfg.Deprecation{Schema: declcfg.SchemaDeprecation, Package: pkgName}, deprecation)

	// Only a genuinely absent package permits a lower-priority catalog.
	w["a"] = func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
		return &declcfg.DeclarativeConfig{}, &ocv1.ClusterCatalogSpec{Priority: 1}, nil
	}
	consulted = nil
	_, gotVersion, _, err = r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("1.1.0")}, *gotVersion)
	assert.Equal(t, []string{"a", "b"}, consulted)

	// Preserve the existing definition: nonempty config means package found.
	w["a"] = func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
		return &declcfg.DeclarativeConfig{Channels: []declcfg.Channel{{Package: pkgName, Name: "alpha"}}}, &ocv1.ClusterCatalogSpec{Priority: 1}, nil
	}
	consulted = nil
	_, _, deprecation, err = r.Resolve(context.Background(), ce, nil)
	require.ErrorContains(t, err, `from catalog "a": no bundles found`)
	assert.Equal(t, []string{"a"}, consulted)
	assert.Equal(t, &declcfg.Deprecation{Schema: declcfg.SchemaDeprecation, Package: pkgName}, deprecation)
}

func TestMultiplePriority(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), &ocv1.ClusterCatalogSpec{Priority: 1}, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), &ocv1.ClusterCatalogSpec{Priority: 0}, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{Packages: []declcfg.Package{{Name: pkgName}}}, &ocv1.ClusterCatalogSpec{Priority: 1}, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}

	// "a" has an eligible bundle; "c" has no bundles but still creates an ambiguity.
	ce := buildFooClusterExtension(pkgName, nil, "3.0.0", ocv1.UpgradeConstraintPolicyCatalogProvided)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "in multiple catalogs with the same priority 1: [a c]")
	assert.Nil(t, gotBundle)
	assert.Nil(t, gotVersion)
	assert.Nil(t, gotDeprecation)
}

func priorityCatalog(name string, priority int32) ocv1.ClusterCatalog {
	return ocv1.ClusterCatalog{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: ocv1.ClusterCatalogSpec{Priority: priority}}
}

func TestGroupCatalogsByPriority(t *testing.T) {
	catalogs := []ocv1.ClusterCatalog{
		priorityCatalog("lowest", -2147483648),
		priorityCatalog("z", 0),
		priorityCatalog("highest", 2147483647),
		priorityCatalog("a", 0),
	}
	groups := groupCatalogsByPriority(catalogs)
	names := make([][]string, 0, len(groups))
	for _, group := range groups {
		groupNames := make([]string, 0, len(group))
		for _, cat := range group {
			groupNames = append(groupNames, cat.Name)
		}
		names = append(names, groupNames)
	}
	assert.Equal(t, [][]string{{"highest"}, {"a", "z"}, {"lowest"}}, names)
	assert.Empty(t, groupCatalogsByPriority(nil))
}

func TestMultipleChannels(t *testing.T) {
	pkgName := randPkg()
	w := staticCatalogs{
		"a": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"b": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return &declcfg.DeclarativeConfig{}, nil, nil
		},
		"c": func() (*declcfg.DeclarativeConfig, *ocv1.ClusterCatalogSpec, error) {
			return genPackage(pkgName), nil, nil
		},
	}
	r := CatalogResolver{ListCatalogsFunc: w.ListCatalogs, GetPackageFunc: w.GetPackage}
	ce := buildFooClusterExtension(pkgName, []string{"beta", "alpha"}, "", ocv1.UpgradeConstraintPolicyCatalogProvided)
	gotBundle, gotVersion, gotDeprecation, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	assert.Equal(t, genBundle(pkgName, "2.0.0"), *gotBundle)
	assert.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("2.0.0")}, *gotVersion)
	assert.Equal(t, ptr.To(packageDeprecation(pkgName)), gotDeprecation)
}

func TestAllCatalogsDisabled(t *testing.T) {
	pkgName := randPkg()
	listCatalogs := func(ctx context.Context, options ...client.ListOption) ([]ocv1.ClusterCatalog, error) {
		return []ocv1.ClusterCatalog{
			{
				Spec: ocv1.ClusterCatalogSpec{
					AvailabilityMode: ocv1.AvailabilityModeUnavailable,
				},
			},
			{
				Spec: ocv1.ClusterCatalogSpec{
					AvailabilityMode: ocv1.AvailabilityModeUnavailable,
				},
			},
		}, nil
	}

	getPackage := func(ctx context.Context, cat *ocv1.ClusterCatalog, packageName string) (*declcfg.DeclarativeConfig, error) {
		panic("getPackage should never be called when all catalogs are disabled")
	}

	r := CatalogResolver{
		ListCatalogsFunc: listCatalogs, GetPackageFunc: getPackage,
	}

	ce := buildFooClusterExtension(pkgName, []string{}, ">=1.0.0", ocv1.UpgradeConstraintPolicyCatalogProvided)
	_, _, _, err := r.Resolve(context.Background(), ce, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in any eligible catalog")
}

func TestSomeCatalogsDisabled(t *testing.T) {
	pkgName := randPkg()
	var consulted []string
	listCatalogs := func(ctx context.Context, options ...client.ListOption) ([]ocv1.ClusterCatalog, error) {
		return []ocv1.ClusterCatalog{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "enabledCatalog",
				},
				Spec: ocv1.ClusterCatalogSpec{
					Priority:         1, // Higher priority
					AvailabilityMode: ocv1.AvailabilityModeAvailable,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "disabledCatalog",
				},
				Spec: ocv1.ClusterCatalogSpec{
					Priority:         0, // Lower priority (but disabled)
					AvailabilityMode: ocv1.AvailabilityModeUnavailable,
				},
			},
		}, nil
	}

	getPackage := func(ctx context.Context, cat *ocv1.ClusterCatalog, packageName string) (*declcfg.DeclarativeConfig, error) {
		consulted = append(consulted, cat.Name)
		return genPackage(pkgName), nil
	}

	r := CatalogResolver{
		ListCatalogsFunc: listCatalogs, GetPackageFunc: getPackage,
	}

	ce := buildFooClusterExtension(pkgName, []string{}, ">=1.0.0", ocv1.UpgradeConstraintPolicyCatalogProvided)
	gotBundle, gotVersion, _, err := r.Resolve(context.Background(), ce, nil)
	require.NoError(t, err)
	require.NotNil(t, gotBundle)
	require.Equal(t, declcfg.VersionRelease{Version: bsemver.MustParse("3.0.0")}, *gotVersion)
	require.Equal(t, []string{"enabledCatalog"}, consulted)
}
