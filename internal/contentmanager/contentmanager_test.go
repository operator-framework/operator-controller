package contentmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
)

func TestWatch(t *testing.T) {
	tests := []struct {
		name    string
		rcm     RestConfigMapper
		config  *rest.Config
		ce      *v1alpha1.ClusterExtension
		objs    []client.Object
		wantErr bool
	}{
		{
			name: "Valid cluster extension valid managed content should pass",
			rcm: func(_ context.Context, _ client.Object, cfg *rest.Config) (*rest.Config, error) {
				return cfg, nil
			},
			config: &rest.Config{},
			ce: &v1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-extension",
				},
			},
			objs: []client.Object{
				&corev1.Pod{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Pod",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "webserver",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Fail when the rest config mapper returns an error",
			rcm: func(_ context.Context, _ client.Object, cfg *rest.Config) (*rest.Config, error) {
				return nil, errors.New("failed getting rest config")
			},
			config: &rest.Config{},
			ce: &v1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-extension",
				},
			},
			objs: []client.Object{
				&corev1.Pod{},
			},
			wantErr: true,
		},
		{
			name: "Should return an error when buildScheme() fails",
			rcm: func(_ context.Context, _ client.Object, cfg *rest.Config) (*rest.Config, error) {
				return cfg, nil
			},
			config: &rest.Config{},
			ce: &v1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-extension",
				},
			},
			objs: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "webserver",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mgr, _ := manager.New(tc.config, manager.Options{})
			ctrl, err := controller.New("test-controller", mgr, controller.Options{
				Reconciler: reconcile.Func(func(context.Context, reconcile.Request) (reconcile.Result, error) {
					return reconcile.Result{}, nil
				}),
			})
			require.NoError(t, err)

			instance := New(tc.rcm, tc.config, mgr.GetRESTMapper())
			got := instance.Watch(context.Background(), ctrl, tc.ce, tc.objs)
			assert.Equal(t, got != nil, tc.wantErr)
		})
	}
}

func TestBuildScheme(t *testing.T) {
	type validation struct {
		gvks  []schema.GroupVersionKind
		valid bool
	}

	testcases := []struct {
		name    string
		objects []client.Object
		wantErr bool
		want    validation
	}{
		{
			name:    "Gvk is not defined",
			objects: []client.Object{&corev1.Pod{}},
			wantErr: true,
			want: validation{
				gvks:  []schema.GroupVersionKind{},
				valid: false,
			},
		},
		{
			name: "Check objects added in scheme",
			objects: []client.Object{
				&appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "webserver",
					},
				},
			},
			wantErr: false,
			want: validation{
				gvks: []schema.GroupVersionKind{
					appsv1.SchemeGroupVersion.WithKind("Deployment"),
				},
				valid: true,
			},
		},
		{
			name: "Check object not defined in scheme",
			objects: []client.Object{
				&corev1.Pod{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Pod",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "webserver",
					},
				},
			},
			wantErr: false,
			want: validation{
				gvks: []schema.GroupVersionKind{
					corev1.SchemeGroupVersion.WithKind("Secret"),
				},
				valid: false,
			},
		},
		{
			name: "Check if empty Group is valid",
			objects: []client.Object{
				&corev1.Pod{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Pod",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "webserver",
					},
				},
			},
			wantErr: false,
			want: validation{
				gvks: []schema.GroupVersionKind{
					corev1.SchemeGroupVersion.WithKind("Pod"),
				},
				valid: true,
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			scheme, err := buildScheme(tc.objects)
			require.Equal(t, err != nil, tc.wantErr)
			for _, gvk := range tc.want.gvks {
				got := scheme.Recognizes(gvk)
				assert.Equal(t, got, tc.want.valid)
			}
		})
	}
}
