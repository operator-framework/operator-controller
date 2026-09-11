package controllers

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	bundlecsv "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
)

// TestValidateInstallNamespace_SystemManaged covers upgrades of an extension whose namespace is
// system-managed. The name is resolved from bundle metadata, which changes between bundles, so an
// upgrade must not be allowed to move an installed extension: the resources would be stranded in
// the original namespace, which is then deleted along with the archived revision.
func TestValidateInstallNamespace_SystemManaged(t *testing.T) {
	const extName = "test-extension"

	bundleFSSuggesting := func(ns string) fstest.MapFS {
		return bundlefs.Builder().WithPackageName("test").
			WithCSV(bundlecsv.Builder().
				WithName("test-csv").
				WithAnnotations(map[string]string{render.AnnotationSuggestedNamespace: ns}).
				Build()).
			Build()
	}
	// managedNamespace is a namespace OLM rendered for this extension: every rendered object
	// carries the owner labels, which is what makes the current namespace discoverable.
	managedNamespace := func(name string) *corev1.Namespace {
		return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				labels.OwnerKindKey: ocv1.ClusterExtensionKind,
				labels.OwnerNameKey: extName,
			},
		}}
	}

	for _, tc := range []struct {
		name             string
		specNamespace    string
		existing         []runtime.Object
		bundleSuggests   string
		noBundleContent  bool
		expectErr        bool
		errContains      string
		expectedTerminal bool
	}{
		{
			name:           "first install resolves freely, nothing is installed yet",
			bundleSuggests: "first-ns",
		},
		{
			// UnpackBundle clears imageFS when the resolved bundle is unchanged but its content
			// cannot be pulled. The namespace cannot have moved in that case, so there is nothing
			// to validate and no bundle to read.
			name:            "unavailable bundle content is not validated",
			existing:        []runtime.Object{managedNamespace("original-ns")},
			noBundleContent: true,
		},
		{
			name:           "upgrade is allowed when the bundle still resolves to the same namespace",
			existing:       []runtime.Object{managedNamespace("original-ns")},
			bundleSuggests: "original-ns",
		},
		{
			name:             "upgrade is rejected when the bundle resolves elsewhere",
			existing:         []runtime.Object{managedNamespace("original-ns")},
			bundleSuggests:   "new-ns",
			expectErr:        true,
			errContains:      "cannot change after installation",
			expectedTerminal: true,
		},
		{
			name:           "namespaces owned by other extensions are ignored",
			existing:       []runtime.Object{&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other-ns", Labels: map[string]string{labels.OwnerKindKey: ocv1.ClusterExtensionKind, labels.OwnerNameKey: "other-extension"}}}},
			bundleSuggests: "new-ns",
		},
		{
			name:           "a user-managed spec.namespace is never bundle-resolved, so it cannot conflict",
			specNamespace:  "user-ns",
			existing:       []runtime.Object{&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "user-ns"}}, managedNamespace("original-ns")},
			bundleSuggests: "new-ns",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ext := &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{Name: extName},
				Spec:       ocv1.ClusterExtensionSpec{Namespace: tc.specNamespace},
			}
			state := &reconcileState{}
			if !tc.noBundleContent {
				state.imageFS = bundleFSSuggesting(tc.bundleSuggests)
			}

			stepFunc := ValidateInstallNamespace(k8sfake.NewClientset(tc.existing...).CoreV1())
			_, err := stepFunc(context.Background(), state, ext)

			if !tc.expectErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errContains)
			if tc.expectedTerminal {
				require.ErrorIs(t, err, reconcile.TerminalError(nil), "a namespace change is deterministic, so it should be terminal")
			}

			progressing := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
			require.NotNil(t, progressing, "the reason the extension is blocked must be reported in status")
			require.Contains(t, progressing.Message, tc.errContains)
		})
	}
}
