package crdupgradesafety_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	kappcus "carvel.dev/kapp/pkg/kapp/crdupgradesafety"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/operator-framework/operator-controller/internal/rukpak/preflights/crdupgradesafety"
	"github.com/operator-framework/operator-controller/internal/rukpak/util"
)

type MockCRDGetter struct {
	oldCrd *apiextensionsv1.CustomResourceDefinition
	getErr error
	apiextensionsv1client.CustomResourceDefinitionInterface
}

func (c *MockCRDGetter) Get(ctx context.Context, name string, options metav1.GetOptions) (*apiextensionsv1.CustomResourceDefinition, error) {
	return c.oldCrd, c.getErr
}

func newMockPreflight(crd *apiextensionsv1.CustomResourceDefinition, err error, customValidator *kappcus.Validator) *crdupgradesafety.Preflight {
	var preflightOpts []crdupgradesafety.Option
	if customValidator != nil {
		preflightOpts = append(preflightOpts, crdupgradesafety.WithValidator(customValidator))
	}
	return crdupgradesafety.NewPreflight(&MockCRDGetter{
		oldCrd: crd,
		getErr: err,
	}, preflightOpts...)
}

const crdFolder string = "../../../../testdata/manifests"

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

// TestInstall exists only for completeness as Install() is currently a no-op. It can be used as
// a template for real tests in the future if the func is implemented.
func TestInstall(t *testing.T) {
	tests := []struct {
		name          string
		oldCrdPath    string
		validator     *kappcus.Validator
		release       *release.Release
		wantErrMsgs   []string
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
			wantErrMsgs: []string{"json: cannot unmarshal string into Go value of type unstructured.detector"},
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
			wantErrMsgs:   []string{"error!"},
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
			wantErrMsgs: []string{"json: cannot unmarshal"},
		},
		{
			name:       "custom validator",
			oldCrdPath: "old-crd.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "old-crd.json"),
			},
			validator: &kappcus.Validator{
				Validations: []kappcus.Validation{
					kappcus.NewValidationFunc("test", func(old, new apiextensionsv1.CustomResourceDefinition) error {
						return fmt.Errorf("custom validation error!!")
					}),
				},
			},
			wantErrMsgs: []string{"custom validation error!!"},
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
			wantErrMsgs: []string{
				`"NoScopeChange"`,
				`"NoStoredVersionRemoved"`,
				`enum constraints`,
				`new required fields`,
				`maximum: constraint`,
				`maxItems: constraint`,
				`maxLength: constraint`,
				`maxProperties: constraint`,
				`minimum: constraint`,
				`minItems: constraint`,
				`minLength: constraint`,
				`minProperties: constraint`,
				`default value`,
			},
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
			wantErrMsgs: []string{
				`"NoExistingFieldRemoved"`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preflight := newMockPreflight(getCrdFromManifestFile(t, tc.oldCrdPath), tc.wantCrdGetErr, tc.validator)
			err := preflight.Install(context.Background(), tc.release)
			if len(tc.wantErrMsgs) != 0 {
				for _, expectedErrMsg := range tc.wantErrMsgs {
					require.ErrorContainsf(t, err, expectedErrMsg, "")
				}
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
		validator     *kappcus.Validator
		release       *release.Release
		wantErrMsgs   []string
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
			wantErrMsgs: []string{"json: cannot unmarshal string into Go value of type unstructured.detector"},
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
			wantErrMsgs:   []string{"error!"},
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
			wantErrMsgs: []string{"json: cannot unmarshal"},
		},
		{
			name:       "custom validator",
			oldCrdPath: "old-crd.json",
			release: &release.Release{
				Name:     "test-release",
				Manifest: getManifestString(t, "old-crd.json"),
			},
			validator: &kappcus.Validator{
				Validations: []kappcus.Validation{
					kappcus.NewValidationFunc("test", func(old, new apiextensionsv1.CustomResourceDefinition) error {
						return fmt.Errorf("custom validation error!!")
					}),
				},
			},
			wantErrMsgs: []string{"custom validation error!!"},
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
			wantErrMsgs: []string{
				`"NoScopeChange"`,
				`"NoStoredVersionRemoved"`,
				`enum constraints`,
				`new required fields`,
				`maximum: constraint`,
				`maxItems: constraint`,
				`maxLength: constraint`,
				`maxProperties: constraint`,
				`minimum: constraint`,
				`minItems: constraint`,
				`minLength: constraint`,
				`minProperties: constraint`,
				`default value`,
			},
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
			wantErrMsgs: []string{
				`"NoExistingFieldRemoved"`,
			},
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
			wantErrMsgs: []string{
				`"ServedVersionValidator" validation failed: version upgrade "v1" to "v2", field "^.spec.foobarbaz": enums`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preflight := newMockPreflight(getCrdFromManifestFile(t, tc.oldCrdPath), tc.wantCrdGetErr, tc.validator)
			err := preflight.Upgrade(context.Background(), tc.release)
			if len(tc.wantErrMsgs) != 0 {
				for _, expectedErrMsg := range tc.wantErrMsgs {
					require.ErrorContainsf(t, err, expectedErrMsg, "")
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
