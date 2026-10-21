package applier_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/testutil"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
)

func TestBoxcutter_Apply_RevisionBounds(t *testing.T) {
	for _, tc := range []struct {
		name      string
		latest    int64
		changed   bool
		exhausted bool
	}{
		{
			name:      "allocates final allowed revision across owners",
			latest:    ocv1.ClusterObjectSetMaxRevision - 1,
			changed:   true,
			exhausted: false,
		},
		{
			name:      "exhaustion preserves history and creates nothing",
			latest:    ocv1.ClusterObjectSetMaxRevision,
			changed:   true,
			exhausted: true,
		},
		{
			name:      "unchanged content at maximum still reconciles",
			latest:    ocv1.ClusterObjectSetMaxRevision,
			changed:   false,
			exhausted: false,
		},
		{
			name:      "out of range stored revision cannot overflow",
			latest:    math.MaxInt64,
			changed:   true,
			exhausted: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRevisionBoundsFixture(t, tc.latest, tc.changed)

			completed, status, err := fixture.applier.Apply(t.Context(), fstest.MapFS{}, fixture.extension, nil, nil)

			require.Empty(t, status)
			require.Equal(t, 1, fixture.requests.comparisons)
			if tc.exhausted {
				require.ErrorContains(t, err, fmt.Sprintf("revision limit %d reached for group %q", ocv1.ClusterObjectSetMaxRevision, fixture.extension.Name))
				require.False(t, completed)
			} else {
				require.NoError(t, err)
				require.True(t, completed)
			}

			if tc.exhausted || !tc.changed {
				require.Zero(t, fixture.requests.newApplies)
				require.Zero(t, fixture.requests.creates)
				require.Zero(t, fixture.requests.deletes)
				require.Zero(t, fixture.requests.patches)
				for _, obj := range fixture.seededObjects {
					retained := obj.DeepCopyObject().(client.Object)
					require.NoError(t, fixture.client.Get(t.Context(), client.ObjectKeyFromObject(obj), retained))
				}
				return
			}

			require.Equal(t, 1, fixture.requests.newApplies)
			require.Positive(t, fixture.requests.creates, "new revision content must be externalized")
			var created ocv1.ClusterObjectSet
			key := client.ObjectKey{
				Name: fmt.Sprintf("%s-%d", fixture.extension.Name, ocv1.ClusterObjectSetMaxRevision),
			}
			require.NoError(t, fixture.client.Get(t.Context(), key, &created))
			require.Len(t, created.Name, 63)
			require.Equal(t, ocv1.ClusterObjectSetMaxRevision, created.Spec.Revision)
			require.Equal(t, fixture.extension.Name, created.Spec.Group)
		})
	}
}

type revisionBoundsFixture struct {
	extension     *ocv1.ClusterExtension
	applier       *applier.Boxcutter
	client        client.Client
	seededObjects []client.Object
	requests      revisionBoundsRequests
}

type revisionBoundsRequests struct {
	comparisons int
	newApplies  int
	creates     int
	deletes     int
	patches     int
}

// newRevisionBoundsFixture seeds history and externalized content, and models
// API immutability rejection when changed content requires another revision.
func newRevisionBoundsFixture(t *testing.T, latestRevision int64, changed bool) *revisionBoundsFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	fixture := &revisionBoundsFixture{
		extension: &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("a", 52),
				UID:  "extension-uid",
			},
		},
	}
	ext := fixture.extension
	owner := *metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind))
	// Enough archived revisions to trigger retention if the guard runs too late.
	for revision := int64(1); revision <= int64(applier.ClusterObjectSetRetentionLimit)+1; revision++ {
		fixture.seededObjects = append(fixture.seededObjects, &ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("%s-%d", ext.Name, revision),
				OwnerReferences: []metav1.OwnerReference{owner},
			},
			Spec: ocv1.ClusterObjectSetSpec{
				Group:               ext.Name,
				Revision:            revision,
				LifecycleState:      ocv1.ClusterObjectSetLifecycleStateArchived,
				CollisionProtection: ocv1.CollisionProtectionPrevent,
			},
		})
	}
	latest := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%d", ext.Name, latestRevision),
			UID:  "latest-uid",
		},
		Spec: ocv1.ClusterObjectSetSpec{
			Group:               ext.Name,
			Revision:            latestRevision,
			LifecycleState:      ocv1.ClusterObjectSetLifecycleStateActive,
			CollisionProtection: ocv1.CollisionProtectionPrevent,
		},
	}
	// The latest revision is deliberately owned by someone else (or unowned).
	// Neither group-wide numbering nor exhaustion may depend on CE ownership.
	if changed {
		foreignOwner := owner
		foreignOwner.Name = "other-extension"
		foreignOwner.UID = "other-uid"
		latest.OwnerReferences = []metav1.OwnerReference{foreignOwner}
	}
	payload := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name": "payload",
			},
		},
	}
	packer := applier.SecretPacker{
		RevisionName:    latest.Name,
		OwnerName:       ext.Name,
		SystemNamespace: "olmv1-system",
	}
	pack, err := packer.Pack([]ocv1.ClusterObjectSetPhase{
		{
			Name: "deploy",
			Objects: []ocv1.ClusterObjectSetObject{
				{
					Object: payload,
				},
			},
		},
	})
	require.NoError(t, err)
	latest.Spec.Phases = []ocv1.ClusterObjectSetPhase{
		{
			Name: "deploy",
			Objects: []ocv1.ClusterObjectSetObject{
				{
					Ref: pack.Refs[[2]int{0, 0}],
				},
			},
		},
	}
	fixture.seededObjects = append(fixture.seededObjects, latest)
	for i := range pack.Secrets {
		pack.Secrets[i].OwnerReferences = []metav1.OwnerReference{
			*metav1.NewControllerRef(latest, ocv1.GroupVersion.WithKind(ocv1.ClusterObjectSetKind)),
		}
		fixture.seededObjects = append(fixture.seededObjects, &pack.Secrets[i])
	}

	fixture.client = testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithObjects(fixture.seededObjects...).WithInterceptorFuncs(interceptor.Funcs{
		Apply: func(ctx context.Context, cl client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
			cos := obj.(*ocv1ac.ClusterObjectSetApplyConfiguration)
			if *cos.Name == latest.Name {
				fixture.requests.comparisons++
				if changed {
					return apierrors.NewInvalid(ocv1.GroupVersion.WithKind(ocv1.ClusterObjectSetKind).GroupKind(), latest.Name,
						field.ErrorList{field.Invalid(field.NewPath("spec", "phases"), "changed", "phases is immutable")})
				}
			} else {
				fixture.requests.newApplies++
			}
			return cl.Apply(ctx, obj, opts...)
		},
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			fixture.requests.creates++
			return cl.Create(ctx, obj, opts...)
		},
		Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			fixture.requests.deletes++
			return cl.Delete(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			fixture.requests.patches++
			return cl.Patch(ctx, obj, patch, opts...)
		},
	}).Build()
	generator := mockapplier.NewMockClusterObjectSetGenerator(gomock.NewController(t))
	generator.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), ext, gomock.Any(), gomock.Any()).Return(
		ocv1ac.ClusterObjectSet("").WithSpec(ocv1ac.ClusterObjectSetSpec().
			WithGroup(ext.Name).
			WithLifecycleState(ocv1.ClusterObjectSetLifecycleStateActive).
			WithCollisionProtection(ocv1.CollisionProtectionPrevent).
			WithPhases(ocv1ac.ClusterObjectSetPhase().
				WithName("deploy").
				WithObjects(ocv1ac.ClusterObjectSetObject().WithObject(payload)))), nil)
	fixture.applier = &applier.Boxcutter{
		Client:            fixture.client,
		Scheme:            scheme,
		RevisionGenerator: generator,
		FieldOwner:        "test-owner",
		SystemNamespace:   "olmv1-system",
	}
	return fixture
}
