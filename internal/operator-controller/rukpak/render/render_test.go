package render_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
)

func Test_BundleRenderer_NoConfig(t *testing.T) {
	renderer := render.BundleRenderer{}
	objs, err := renderer.Render(
		bundle.RegistryV1{
			CSV: MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
		}, "", nil)
	require.NoError(t, err)
	require.Empty(t, objs)
}

func Test_BundleRenderer_ValidatesBundle(t *testing.T) {
	renderer := render.BundleRenderer{
		BundleValidator: render.BundleValidator{
			func(v1 *bundle.RegistryV1) []error {
				return []error{errors.New("this bundle is invalid")}
			},
		},
	}
	objs, err := renderer.Render(bundle.RegistryV1{}, "")
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "this bundle is invalid")
}

func Test_BundleRenderer_CreatesCorrectDefaultOptions(t *testing.T) {
	expectedInstallNamespace := "install-namespace"
	expectedTargetNamespaces := []string{""}
	expectedUniqueNameGenerator := render.DefaultUniqueNameGenerator

	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
				require.Equal(t, expectedInstallNamespace, opts.InstallNamespace)
				require.Equal(t, expectedTargetNamespaces, opts.TargetNamespaces)
				require.Equal(t, reflect.ValueOf(expectedUniqueNameGenerator).Pointer(), reflect.ValueOf(render.DefaultUniqueNameGenerator).Pointer(), "options has unexpected default unique name generator")
				return nil, nil
			},
		},
	}

	_, _ = renderer.Render(bundle.RegistryV1{}, expectedInstallNamespace)
}

func Test_BundleRenderer_ValidatesRenderOptions(t *testing.T) {
	for _, tc := range []struct {
		name             string
		installNamespace string
		csv              v1alpha1.ClusterServiceVersion
		opts             []render.Option
		err              error
	}{
		{
			name:             "rejects empty targetNamespaces",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
			opts: []render.Option{
				render.WithTargetNamespaces(),
			},
			err: errors.New("invalid option(s): at least one target namespace must be specified"),
		}, {
			name:             "rejects nil unique name generator",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
			opts: []render.Option{
				render.WithUniqueNameGenerator(nil),
			},
			err: errors.New("invalid option(s): unique name generator must be specified"),
		}, {
			name:             "rejects all namespace install if AllNamespaces install mode is not supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces(corev1.NamespaceAll),
			},
			err: errors.New("invalid option(s): invalid target namespaces []: supported install modes [SingleNamespace] do not support targeting all namespaces"),
		}, {
			name:             "rejects own namespace install if only AllNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
			opts: []render.Option{
				render.WithTargetNamespaces("install-namespace"),
			},
			err: errors.New("invalid option(s): invalid target namespaces [install-namespace]: supported install modes [AllNamespaces] do not support target namespaces [install-namespace]"),
		}, {
			name:             "rejects install out of own namespace if only OwnNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("not-install-namespace"),
			},
			err: errors.New("invalid option(s): invalid target namespaces [not-install-namespace]: supported install modes [OwnNamespace] do not support target namespaces [not-install-namespace]"),
		}, {
			name:             "rejects multi-namespace install if MultiNamespace install mode is not supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
			opts: []render.Option{
				render.WithTargetNamespaces("ns1", "ns2", "ns3"),
			},
			err: errors.New("invalid option(s): invalid target namespaces [ns1 ns2 ns3]: supported install modes [AllNamespaces] do not support target namespaces [ns1 ns2 ns3]"),
		}, {
			name:             "rejects if bundle supports no install modes",
			installNamespace: "install-namespace",
			csv:              MakeCSV(),
			opts: []render.Option{
				render.WithTargetNamespaces("some-namespace"),
			},
			err: errors.New("invalid option(s): invalid target namespaces [some-namespace]: supported install modes [] do not support target namespaces [some-namespace]"),
		}, {
			name:             "accepts all namespace render if AllNamespaces install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
			opts: []render.Option{
				render.WithTargetNamespaces(""),
			},
		}, {
			name:             "accepts install namespace render if SingleNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("some-namespace"),
			},
		}, {
			name:             "accepts all install namespace render if OwnNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("install-namespace"),
			},
		}, {
			name:             "accepts single namespace render if SingleNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("some-namespace"),
			},
		}, {
			name:             "accepts multi namespace render if MultiNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeMultiNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("n1", "n2", "n3"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			renderer := render.BundleRenderer{}
			_, err := renderer.Render(
				bundle.RegistryV1{CSV: tc.csv},
				tc.installNamespace,
				tc.opts...,
			)
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, tc.err.Error(), err.Error())
			}
		})
	}
}

func Test_BundleRenderer_AppliesUserOptions(t *testing.T) {
	isOptionApplied := false
	_, _ = render.BundleRenderer{}.Render(bundle.RegistryV1{}, "install-namespace", func(options *render.Options) {
		isOptionApplied = true
	})
	require.True(t, isOptionApplied)
}

func Test_WithTargetNamespaces(t *testing.T) {
	opts := &render.Options{
		TargetNamespaces: []string{"target-namespace"},
	}
	render.WithTargetNamespaces("a", "b", "c")(opts)
	require.Equal(t, []string{"a", "b", "c"}, opts.TargetNamespaces)
}

func Test_WithUniqueNameGenerator(t *testing.T) {
	opts := &render.Options{
		UniqueNameGenerator: render.DefaultUniqueNameGenerator,
	}
	render.WithUniqueNameGenerator(func(s string, i interface{}) (string, error) {
		return "a man needs a name", nil
	})(opts)
	generatedName, _ := opts.UniqueNameGenerator("", nil)
	require.Equal(t, "a man needs a name", generatedName)
}

func Test_WithCertificateProvide(t *testing.T) {
	opts := &render.Options{}
	expectedCertProvider := FakeCertProvider{}
	render.WithCertificateProvider(expectedCertProvider)(opts)
	require.Equal(t, expectedCertProvider, opts.CertificateProvider)
}

func Test_BundleRenderer_CallsResourceGenerators(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
			func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&appsv1.Deployment{}}, nil
			},
		},
	}
	objs, err := renderer.Render(
		bundle.RegistryV1{
			CSV: MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
		}, "")
	require.NoError(t, err)
	require.Equal(t, []client.Object{&corev1.Namespace{}, &corev1.Service{}, &appsv1.Deployment{}}, objs)
}

func Test_BundleRenderer_ReturnsResourceGeneratorErrors(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
			func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
				return nil, fmt.Errorf("generator error")
			},
		},
	}
	objs, err := renderer.Render(
		bundle.RegistryV1{
			CSV: MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
		}, "")
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "generator error")
}

func Test_BundleValidatorCallsAllValidationFnsInOrder(t *testing.T) {
	actual := ""
	val := render.BundleValidator{
		func(v1 *bundle.RegistryV1) []error {
			actual += "h"
			return nil
		},
		func(v1 *bundle.RegistryV1) []error {
			actual += "i"
			return nil
		},
	}
	require.NoError(t, val.Validate(nil))
	require.Equal(t, "hi", actual)
}

// Test_Render_ValidatesOutputForAllInstallModes verifies that the BundleRenderer
// generates the correct set of Kubernetes resources (client.Objects) for each supported
// install mode: AllNamespaces, SingleNamespace, and OwnNamespace.
//
// For each mode, it checks that:
// - All expected resources are returned.
// - The full content of each resource matches the expected values
// It validates that the rendered objects are correctly rendered.
func Test_Render_ValidatesOutputForAllInstallModes(t *testing.T) {
	testCases := []struct {
		name             string
		installNamespace string
		watchNamespace   string
		installModes     []v1alpha1.InstallMode
		expectedNS       string
	}{
		{
			name:             "AllNamespaces",
			installNamespace: "mock-system",
			watchNamespace:   "",
			installModes: []v1alpha1.InstallMode{
				{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true},
			},
			expectedNS: "mock-system",
		},
		{
			name:             "SingleNamespace",
			installNamespace: "mock-system",
			watchNamespace:   "mock-watch",
			installModes: []v1alpha1.InstallMode{
				{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true},
			},
			expectedNS: "mock-watch",
		},
		{
			name:             "OwnNamespace",
			installNamespace: "mock-system",
			watchNamespace:   "mock-system",
			installModes: []v1alpha1.InstallMode{
				{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
			},
			expectedNS: "mock-system",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given the mock scenarios
			expectedObjects := []client.Object{
				fakeUnstructured("ClusterRole", "", "mock-clusterrole"),
				fakeUnstructured("ClusterRoleBinding", "", "mock-clusterrolebinding"),
				fakeUnstructured("Role", tc.expectedNS, "mock-role"),
				fakeUnstructured("RoleBinding", tc.expectedNS, "mock-rolebinding"),
				fakeUnstructured("ConfigMap", tc.expectedNS, "mock-config"),
				fakeUnstructured("Secret", tc.expectedNS, "mock-secret"),
				fakeUnstructured("Service", tc.expectedNS, "mock-service"),
				fakeUnstructured("Deployment", tc.expectedNS, "mock-deployment"),
				fakeUnstructured("ServiceAccount", tc.expectedNS, "mock-sa"),
				fakeUnstructured("NetworkPolicy", tc.expectedNS, "mock-netpol"),
			}

			mockGen := render.ResourceGenerator(func(_ *bundle.RegistryV1, _ render.Options) ([]client.Object, error) {
				return expectedObjects, nil
			})

			mockBundle := bundle.RegistryV1{
				CSV: v1alpha1.ClusterServiceVersion{
					Spec: v1alpha1.ClusterServiceVersionSpec{
						InstallModes: tc.installModes,
					},
				},
			}

			// When we call the BundleRenderer with the mock bundle
			renderer := render.BundleRenderer{
				BundleValidator: render.BundleValidator{
					func(_ *bundle.RegistryV1) []error { return nil },
				},
				ResourceGenerators: []render.ResourceGenerator{mockGen},
			}

			opts := []render.Option{
				render.WithTargetNamespaces(tc.watchNamespace),
				render.WithUniqueNameGenerator(render.DefaultUniqueNameGenerator),
			}

			// Then we expect the rendered objects to match the expected objects
			objs, err := renderer.Render(mockBundle, tc.installNamespace, opts...)
			require.NoError(t, err)
			require.Len(t, objs, len(expectedObjects))

			gotMap := make(map[string]client.Object)
			for _, obj := range objs {
				gotMap[objectKey(obj)] = obj
			}

			for _, exp := range expectedObjects {
				key := objectKey(exp)
				got, exists := gotMap[key]
				require.True(t, exists, "missing expected object: %s", key)

				expObj := exp.(*unstructured.Unstructured)
				gotObj := got.(*unstructured.Unstructured)

				if diff := cmp.Diff(expObj.Object, gotObj.Object, cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("object content mismatch for %s (-want +got):\n%s", key, diff)
				}
			}
		})
	}
}

// fakeUnstructured creates a fake unstructured client.Object with the specified kind, namespace, and name
// to allow us mocks resources to be rendered by the BundleRenderer.
func fakeUnstructured(kind, namespace, name string) client.Object {
	obj := &unstructured.Unstructured{}
	obj.Object = make(map[string]interface{})

	group := ""
	version := "v1"

	switch kind {
	case "NetworkPolicy":
		err := unstructured.SetNestedField(obj.Object, map[string]interface{}{
			"podSelector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"app": "my-app"},
			},
			"policyTypes": []interface{}{"Ingress"},
		}, "spec")
		if err != nil {
			panic(fmt.Sprintf("failed to set spec for NetworkPolicy: %v", err))
		}
	case "Service":
		_ = unstructured.SetNestedField(obj.Object, map[string]interface{}{
			"ports": []interface{}{
				map[string]interface{}{
					"port":       int64(8080),
					"targetPort": "http",
				},
			},
			"selector": map[string]interface{}{
				"app": "mock-app",
			},
		}, "spec")
	case "Deployment":
		_ = unstructured.SetNestedField(obj.Object, map[string]interface{}{
			"replicas": int64(1),
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"app": "mock-app"},
			},
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{"app": "mock-app"},
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "controller",
							"image": "mock-controller:latest",
						},
					},
				},
			},
		}, "spec")
	case "ConfigMap":
		_ = unstructured.SetNestedField(obj.Object, map[string]interface{}{
			"controller": "enabled",
		}, "data")
	}

	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   group,
		Version: version,
		Kind:    kind,
	})
	obj.SetNamespace(namespace)
	obj.SetName(name)

	return obj
}

// objectKey returns a unique key for k8s resources
func objectKey(obj client.Object) string {
	gvk := obj.GetObjectKind().GroupVersionKind()
	return fmt.Sprintf("%s/%s/%s", gvk.Kind, obj.GetNamespace(), obj.GetName())
}
