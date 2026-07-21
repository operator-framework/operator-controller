package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	bundlecsv "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
)

// TestResolveNamespaceManaged covers the managed-mode (spec.namespace omitted) branch of
// ResolveNamespace, focusing on the pre-existing-namespace check. The check must reject a
// foreign namespace with a clear terminal error, but must NOT reject a namespace this
// ClusterExtension already owns — otherwise a first install would deadlock against the
// namespace it created a reconcile earlier.
func TestResolveNamespaceManaged(t *testing.T) {
	const managedNS = "my-managed-ns"

	ext := func(name string, installed bool) *ocv1.ClusterExtension {
		e := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog:    &ocv1.CatalogFilter{PackageName: "test-package"},
				},
			},
		}
		if installed {
			e.Status.Install = &ocv1.ClusterExtensionInstallStatus{}
		}
		return e
	}

	ownedNS := func(owner string) *corev1.Namespace {
		return &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   managedNS,
				Labels: map[string]string{labels.OwnerNameKey: owner},
			},
		}
	}

	bundleFS := bundlefs.Builder().
		WithPackageName("test-package").
		WithCSV(bundlecsv.Builder().
			WithName("test-csv").
			WithAnnotations(map[string]string{
				applier.AnnotationSuggestedNamespace: managedNS,
			}).
			Build()).
		Build()

	tests := []struct {
		name             string
		ext              *ocv1.ClusterExtension
		existingNS       *corev1.Namespace
		expectError      bool
		errorMsgIncludes string
	}{
		{
			name: "first install, namespace does not exist",
			ext:  ext("test-ext", false),
		},
		{
			name:       "first install, namespace already owned by us proceeds",
			ext:        ext("test-ext", false),
			existingNS: ownedNS("test-ext"),
		},
		{
			name:             "first install, pre-existing foreign namespace is rejected",
			ext:              ext("test-ext", false),
			existingNS:       ownedNS("some-other-ext"),
			expectError:      true,
			errorMsgIncludes: "already exists and is not managed by this ClusterExtension",
		},
		{
			name:             "first install, unlabeled pre-existing namespace is rejected",
			ext:              ext("test-ext", false),
			existingNS:       &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedNS}},
			expectError:      true,
			errorMsgIncludes: "already exists and is not managed by this ClusterExtension",
		},
		{
			name:       "upgrade skips the existence check",
			ext:        ext("test-ext", true),
			existingNS: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedNS}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			var objs []runtime.Object
			if tt.existingNS != nil {
				objs = append(objs, tt.existingNS)
			}
			nsClient := fake.NewClientset(objs...).CoreV1()

			state := &reconcileState{imageFS: bundleFS}
			step := ResolveNamespace(nsClient)

			_, err := step(ctx, state, tt.ext)

			if tt.expectError {
				require.Error(t, err)
				require.ErrorIs(t, err, reconcile.TerminalError(nil))
				require.Contains(t, err.Error(), tt.errorMsgIncludes)
				return
			}

			require.NoError(t, err)
			require.True(t, state.namespaceManaged)
			require.Equal(t, managedNS, state.resolvedNamespace)
		})
	}
}
