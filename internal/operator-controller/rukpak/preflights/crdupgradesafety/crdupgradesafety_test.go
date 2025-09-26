package crdupgradesafety_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	crdifyconfig "sigs.k8s.io/crdify/pkg/config"

	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/preflights/crdupgradesafety"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

type MockCRDGetter struct {
	oldCrd *apiextensionsv1.CustomResourceDefinition
	getErr error
	apiextensionsv1client.CustomResourceDefinitionInterface
}

func (c *MockCRDGetter) Get(ctx context.Context, name string, options metav1.GetOptions) (*apiextensionsv1.CustomResourceDefinition, error) {
	return c.oldCrd, c.getErr
}

func newMockPreflight(crd *apiextensionsv1.CustomResourceDefinition, err error) *crdupgradesafety.Preflight {
	var preflightOpts []crdupgradesafety.Option
	return crdupgradesafety.NewPreflight(&MockCRDGetter{
		oldCrd: crd,
		getErr: err,
	}, preflightOpts...)
}

const crdFolder string = "testdata/manifests"

func getCrdFromManifestFile(t *testing.T, oldCrdFile string) *apiextensionsv1.CustomResourceDefinition {
	if oldCrdFile == "" {
		return nil
	}
	relObjects, err := util.ManifestObjects(strings.NewReader(getManifestString(t, oldCrdFile)), "old")
	require.NoError(t, err)

	newCrd := &apiextensionsv1.CustomResourceDefinition{}
	for _, obj := range relObjects {
		if obj.GetObjectKind().GroupVersionKind() != apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition") {
			continue
		}
		uMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		require.NoError(t, err)
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uMap, newCrd)
		require.NoError(t, err)
	}
	return newCrd
}

func getManifestString(t *testing.T, crdFile string) string {
	buff, err := os.ReadFile(fmt.Sprintf("%s/%s", crdFolder, crdFile))
	require.NoError(t, err)
	return string(buff)
}

func wantErrorMsgs(wantMsgs []string) require.ErrorAssertionFunc {
	return func(t require.TestingT, haveErr error, _ ...interface{}) {
		for _, wantMsg := range wantMsgs {
			require.ErrorContains(t, haveErr, wantMsg)
		}
	}
}

// TestInstall exists only for completeness as Install() is currently a no-op. It can be used as
// a template for real tests in the future if the func is implemented.
func TestInstall(t *testing.T) {
	tests := []struct {
		name          string
		oldCrdPath    string
		release       *release.Release
		requireErr    require.ErrorAssertionFunc
		wantCrdGetErr error
	}{
		{
			name: "nil release",
		},
		{
			name: "release with no objects",
			release: &release.Release{
				Name: "test-release",
			},
		},
		{
			name: "release with invalid manifest",
			release: &release.Release{
				Name:     "test-release",
				Manifest: "abcd",
			},
			requireErr: wantErrorMsgs([]string{"json: cannot unmarshal string into Go value of type unstructured.detector"}),
		},
		{
			name: "release with no CRD objects",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "no-crds.json"),
			},
		},
		{
			name: "fail to get old crd other than not found error",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-valid-upgrade.json"),
			},
			wantCrdGetErr: fmt.Errorf("error!"),
			requireErr:    wantErrorMsgs([]string{"error!"}),
		},
		{
			name: "fail to get old crd, not found error",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-valid-upgrade.json"),
			},
			wantCrdGetErr: apierrors.NewNotFound(schema.GroupResource{Group: apiextensionsv1.SchemeGroupVersion.Group, Resource: "customresourcedefinitions"}, "not found"),
		},
		{
			name: "invalid crd manifest file",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-invalid"),
			},
			requireErr: wantErrorMsgs([]string{"json: cannot unmarshal"}),
		},
		{
			name:       "valid upgrade",
			oldCrdPath: "old-crd.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-valid-upgrade.json"),
			},
		},
		{
			name: "new crd validation failures (all except existing field removal)",
			// Not really intended to test kapp validators, although it does anyway to a large extent.
			// This test is primarily meant to ensure that we are actually using all of them.
			oldCrdPath: "old-crd.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-invalid-upgrade.json"),
			},
			requireErr: wantErrorMsgs([]string{
				`scope:`,
				`storedVersionRemoval:`,
				`enum:`,
				`required:`,
				`maximum:`,
				`maxItems:`,
				`maxLength:`,
				`maxProperties:`,
				`minimum:`,
				`minItems:`,
				`minLength:`,
				`minProperties:`,
				`default:`,
			}),
		},
		{
			name: "new crd validation failure for existing field removal",
			// Separate test from above as this error will cause the validator to
			// return early and skip some of the above validations.
			oldCrdPath: "old-crd.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-field-removed.json"),
			},
			requireErr: wantErrorMsgs([]string{
				`existingFieldRemoval:`,
			}),
		},
		{
			name: "new crd validation should not fail on description changes",
			// Separate test from above as this error will cause the validator to
			// return early and skip some of the above validations.
			oldCrdPath: "old-crd.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-description-changed.json"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preflight := newMockPreflight(getCrdFromManifestFile(t, tc.oldCrdPath), tc.wantCrdGetErr)
			objs, err := applier.HelmReleaseToObjectsConverter{}.GetObjectsFromRelease(tc.release)
			if err == nil {
				err = preflight.Install(context.Background(), objs)
			}
			if tc.requireErr != nil {
				tc.requireErr(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUpgrade(t *testing.T) {
	tests := []struct {
		name          string
		oldCrdPath    string
		release       *release.Release
		requireErr    require.ErrorAssertionFunc
		wantCrdGetErr error
	}{
		{
			name: "nil release",
		},
		{
			name: "release with no objects",
			release: &release.Release{
				Name: "test-release",
			},
		},
		{
			name: "release with invalid manifest",
			release: &release.Release{
				Name:     "test-release",
				Manifest: "abcd",
			},
			requireErr: wantErrorMsgs([]string{"json: cannot unmarshal string into Go value of type unstructured.detector"}),
		},
		{
			name: "release with no CRD objects",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "no-crds.json"),
			},
		},
		{
			name: "fail to get old crd, other than not found error",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-valid-upgrade.json"),
			},
			wantCrdGetErr: fmt.Errorf("error!"),
			requireErr:    wantErrorMsgs([]string{"error!"}),
		},
		{
			name: "fail to get old crd, not found error",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-valid-upgrade.json"),
			},
			wantCrdGetErr: apierrors.NewNotFound(schema.GroupResource{Group: apiextensionsv1.SchemeGroupVersion.Group, Resource: "customresourcedefinitions"}, "not found"),
		},
		{
			name: "invalid crd manifest file",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-invalid"),
			},
			requireErr: wantErrorMsgs([]string{"json: cannot unmarshal"}),
		},
		{
			name:       "valid upgrade",
			oldCrdPath: "old-crd.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-valid-upgrade.json"),
			},
		},
		{
			name: "new crd validation failures (all except existing field removal)",
			// Not really intended to test kapp validators, although it does anyway to a large extent.
			// This test is primarily meant to ensure that we are actually using all of them.
			oldCrdPath: "old-crd.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-invalid-upgrade.json"),
			},
			requireErr: wantErrorMsgs([]string{
				`scope:`,
				`storedVersionRemoval:`,
				`enum:`,
				`required:`,
				`maximum:`,
				`maxItems:`,
				`maxLength:`,
				`maxProperties:`,
				`minimum:`,
				`minItems:`,
				`minLength:`,
				`minProperties:`,
				`default:`,
			}),
		},
		{
			name: "new crd validation failure for existing field removal",
			// Separate test from above as this error will cause the validator to
			// return early and skip some of the above validations.
			oldCrdPath: "old-crd.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-field-removed.json"),
			},
			requireErr: wantErrorMsgs([]string{
				`existingFieldRemoval:`,
			}),
		},
		{
			name:       "webhook conversion strategy exists",
			oldCrdPath: "crd-conversion-webhook-old.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-conversion-webhook.json"),
			},
		},
		{
			name:       "new crd validation failure when missing conversion strategy and enum values removed",
			oldCrdPath: "crd-conversion-webhook-old.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-conversion-no-webhook.json"),
			},
			requireErr: wantErrorMsgs([]string{
				`validating upgrade for CRD "crontabs.stable.example.com": v1 -> v2: ^.spec.foobarbaz: enum: allowed enum values removed`,
			}),
		},
		{
			name: "new crd validation should not fail on description changes",
			// Separate test from above as this error will cause the validator to
			// return early and skip some of the above validations.
			oldCrdPath: "old-crd.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-description-changed.json"),
			},
		},
		{
			name:       "success when old crd and new crd contain the exact same validation issues",
			oldCrdPath: "crd-conversion-no-webhook.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-conversion-no-webhook.json"),
			},
		},
		{
			name:       "failure when old crd and new crd contain the exact same validation issues, but new crd introduces another validation issue",
			oldCrdPath: "crd-conversion-no-webhook.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "crd-conversion-no-webhook-extra-issue.json"),
			},
			requireErr: func(t require.TestingT, err error, _ ...interface{}) {
				require.ErrorContains(t, err,
					`validating upgrade for CRD "crontabs.stable.example.com":`,
				)
				// The newly introduced issue is reported
				require.Contains(t, err.Error(),
					`v1 -> v2: ^.spec.extraField: type: type changed : "boolean" -> "string"`,
				)
				// The existing issue is not reported
				require.NotContains(t, err.Error(),
					`v1 -> v2: ^.spec.foobarbaz: enum: allowed enum values removed`,
				)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preflight := newMockPreflight(getCrdFromManifestFile(t, tc.oldCrdPath), tc.wantCrdGetErr)
			objs, err := applier.HelmReleaseToObjectsConverter{}.GetObjectsFromRelease(tc.release)
			if err == nil {
				err = preflight.Upgrade(context.Background(), objs)
			}
			if tc.requireErr != nil {
				tc.requireErr(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUpgrade_UnhandledChanges_InSpec_DefaultPolicy(t *testing.T) {
	t.Run("unhandled spec changes cause error by default", func(t *testing.T) {
		preflight := newMockPreflight(getCrdFromManifestFile(t, "crd-unhandled-old.json"), nil)
		rel := &release.Release{
			Name:     "test-release",
			Manifest: getManifestString(t, "crd-unhandled-new.json"),
		}
		objs, err := applier.HelmReleaseToObjectsConverter{}.GetObjectsFromRelease(rel)
		require.NoError(t, err)
		err = preflight.Upgrade(context.Background(), objs)
		require.Error(t, err)
		require.ErrorContains(t, err, "unhandled changes found")
		require.ErrorContains(t, err, "Format \"\" -> \"email\"")
		require.NotContains(t, err.Error(), "v1.JSONSchemaProps", "error message should be concise without raw diff")
	})
}

func TestUpgrade_UnhandledChanges_PolicyError(t *testing.T) {
	t.Run("unhandled changes error when policy is Error", func(t *testing.T) {
		oldCrd := getCrdFromManifestFile(t, "crd-unhandled-old.json")
		preflight := crdupgradesafety.NewPreflight(&MockCRDGetter{oldCrd: oldCrd}, crdupgradesafety.WithConfig(&crdifyconfig.Config{
			Conversion:           crdifyconfig.ConversionPolicyIgnore,
			UnhandledEnforcement: crdifyconfig.EnforcementPolicyError,
		}))

		rel := &release.Release{
			Name:     "test-release",
			Manifest: getManifestString(t, "crd-unhandled-new.json"),
		}

		objs, err := applier.HelmReleaseToObjectsConverter{}.GetObjectsFromRelease(rel)
		require.NoError(t, err)
		err = preflight.Upgrade(context.Background(), objs)
		require.Error(t, err)
		require.ErrorContains(t, err, "unhandled changes found")
		require.ErrorContains(t, err, "Format \"\" -> \"email\"")
	})
}
