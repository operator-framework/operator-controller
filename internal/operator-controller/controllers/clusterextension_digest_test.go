package controllers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
)

func TestCatalogSpecDigest(t *testing.T) {
	tests := []struct {
		name     string
		ext1     *ocv1.ClusterExtension
		ext2     *ocv1.ClusterExtension
		wantSame bool
	}{
		{
			name: "same spec produces same digest",
			ext1: &ocv1.ClusterExtension{
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: ocv1.SourceTypeCatalog,
						Catalog: &ocv1.CatalogFilter{
							PackageName: "test",
							Version:     "1.0.0",
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"olm.operatorframework.io/metadata.name": "test-catalog",
								},
							},
							UpgradeConstraintPolicy: ocv1.UpgradeConstraintPolicySelfCertified,
						},
					},
				},
			},
			ext2: &ocv1.ClusterExtension{
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: ocv1.SourceTypeCatalog,
						Catalog: &ocv1.CatalogFilter{
							PackageName: "test",
							Version:     "1.0.0",
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"olm.operatorframework.io/metadata.name": "test-catalog",
								},
							},
							UpgradeConstraintPolicy: ocv1.UpgradeConstraintPolicySelfCertified,
						},
					},
				},
			},
			wantSame: true,
		},
		{
			name: "different version produces different digest",
			ext1: &ocv1.ClusterExtension{
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: ocv1.SourceTypeCatalog,
						Catalog: &ocv1.CatalogFilter{
							PackageName: "test",
							Version:     "1.0.0",
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"olm.operatorframework.io/metadata.name": "test-catalog",
								},
							},
							UpgradeConstraintPolicy: ocv1.UpgradeConstraintPolicySelfCertified,
						},
					},
				},
			},
			ext2: &ocv1.ClusterExtension{
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: ocv1.SourceTypeCatalog,
						Catalog: &ocv1.CatalogFilter{
							PackageName: "test",
							Version:     "1.0.2",
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"olm.operatorframework.io/metadata.name": "test-catalog",
								},
							},
							UpgradeConstraintPolicy: ocv1.UpgradeConstraintPolicySelfCertified,
						},
					},
				},
			},
			wantSame: false,
		},
		{
			name: "different channels produces different digest",
			ext1: &ocv1.ClusterExtension{
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: ocv1.SourceTypeCatalog,
						Catalog: &ocv1.CatalogFilter{
							PackageName: "test",
							Channels:    []string{"alpha"},
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"olm.operatorframework.io/metadata.name": "test-catalog",
								},
							},
						},
					},
				},
			},
			ext2: &ocv1.ClusterExtension{
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: ocv1.SourceTypeCatalog,
						Catalog: &ocv1.CatalogFilter{
							PackageName: "test",
							Channels:    []string{"beta"},
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"olm.operatorframework.io/metadata.name": "test-catalog",
								},
							},
						},
					},
				},
			},
			wantSame: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			digest1 := controllers.CatalogSpecDigest(tt.ext1)
			digest2 := controllers.CatalogSpecDigest(tt.ext2)

			assert.NotEmpty(t, digest1, "digest1 should not be empty")
			assert.NotEmpty(t, digest2, "digest2 should not be empty")

			if tt.wantSame {
				assert.Equal(t, digest1, digest2, "digests should be equal")
			} else {
				assert.NotEqual(t, digest1, digest2, "digests should be different")
			}
		})
	}
}
