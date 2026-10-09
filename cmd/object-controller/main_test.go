package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/scheme"
	"github.com/operator-framework/operator-controller/test"
)

func TestValidateMetricsFlags(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      config
		wantAddr string
		wantErr  string
	}{
		{name: "disabled"},
		{name: "certificate without key", cfg: config{certFile: "cert"}, wantErr: "must be used together"},
		{name: "key without certificate", cfg: config{keyFile: "key"}, wantErr: "must be used together"},
		{name: "insecure metrics", cfg: config{metricsAddr: ":8443"}, wantErr: "requires tls-cert and tls-key"},
		{name: "default address", cfg: config{certFile: "cert", keyFile: "key"}, wantAddr: ":8443"},
		{name: "custom address", cfg: config{certFile: "cert", keyFile: "key", metricsAddr: ":9443"}, wantAddr: ":9443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.validate()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantAddr, tc.cfg.metricsAddr)
		})
	}
}

func TestVersionWithoutCluster(t *testing.T) {
	cmd := newCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"version"})
	require.NoError(t, cmd.Execute())
	require.NotEmpty(t, output.String())
}

// Exercise the actual manager and revision engine with only the ClusterObjectSet CRD installed.
func TestStandaloneController(t *testing.T) {
	ctrl.SetLogger(testr.New(t))
	testEnv := test.NewEnv()
	testEnv.CRDDirectoryPaths = []string{"../../helm/olmv1/base/object-controller/crd/experimental"}
	restConfig, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, test.StopWithRetry(testEnv, time.Minute, time.Second)) })

	cl, err := client.New(restConfig, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKey{Name: "clusterextensions.olm.operatorframework.io"}, &apiextensionsv1.CustomResourceDefinition{})))
	require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKey{Name: "clustercatalogs.olm.operatorframework.io"}, &apiextensionsv1.CustomResourceDefinition{})))

	mgr, err := newManager(&config{probeAddr: "0", pprofAddr: "0"}, restConfig)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(30 * time.Second):
			t.Error("manager did not stop")
		}
	})
	syncCtx, syncCancel := context.WithTimeout(ctx, 30*time.Second)
	defer syncCancel()
	require.True(t, mgr.GetCache().WaitForCacheSync(syncCtx), "manager cache did not synchronize")

	for _, name := range []string{"inline", "secret-ref", "mutable-secret-ref"} {
		t.Run(name, func(t *testing.T) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "standalone-"}}
			require.NoError(t, cl.Create(ctx, ns))
			manifest := unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1", "kind": "ConfigMap",
				"metadata": map[string]any{"name": name, "namespace": ns.Name},
				"data":     map[string]any{"hello": "world"},
			}}
			obj := ocv1.ClusterObjectSetObject{Object: manifest}
			var secret *corev1.Secret
			if name != "inline" {
				data, err := json.Marshal(manifest.Object)
				require.NoError(t, err)
				secret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "content", Namespace: ns.Name},
					Immutable:  ptr.To(name != "mutable-secret-ref"), Data: map[string][]byte{"object": data},
				}
				require.NoError(t, cl.Create(ctx, secret))
				obj = ocv1.ClusterObjectSetObject{Ref: ocv1.ObjectSourceRef{Name: secret.Name, Namespace: secret.Namespace, Key: "object"}}
			}
			cos := &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec: ocv1.ClusterObjectSetSpec{
					LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive, Revision: 1,
					CollisionProtection: ocv1.CollisionProtectionPrevent,
					Phases:              []ocv1.ClusterObjectSetPhase{{Name: "deploy", Objects: []ocv1.ClusterObjectSetObject{obj}}},
				},
			}
			require.NoError(t, cl.Create(ctx, cos))
			if secret != nil {
				require.NoError(t, controllerutil.SetControllerReference(cos, secret, scheme.Scheme))
				require.NoError(t, cl.Update(ctx, secret))
			}
			rolloutTimeout := time.Minute
			if name == "mutable-secret-ref" {
				require.EventuallyWithT(t, func(collect *assert.CollectT) {
					if !assert.NoError(collect, cl.Get(ctx, client.ObjectKeyFromObject(cos), cos)) {
						return
					}
					condition := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
					if assert.NotNil(collect, condition) {
						assert.Equal(collect, ocv1.ClusterObjectSetReasonBlocked, condition.Reason)
						assert.Contains(collect, condition.Message, "not immutable")
					}
				}, 5*time.Second, 100*time.Millisecond)

				// Let status-triggered reconciliations settle before changing only
				// the Secret. Recovery must precede the 10-second polling retry.
				lastVersion, unchangedSince := cos.ResourceVersion, time.Now()
				require.Eventually(t, func() bool {
					if err := cl.Get(ctx, client.ObjectKeyFromObject(cos), cos); err != nil {
						return false
					}
					if cos.ResourceVersion != lastVersion {
						lastVersion, unchangedSince = cos.ResourceVersion, time.Now()
					}
					return time.Since(unchangedSince) >= time.Second
				}, 3*time.Second, 100*time.Millisecond)
				secret.Immutable = ptr.To(true)
				require.NoError(t, cl.Update(ctx, secret))
				rolloutTimeout = 5 * time.Second
			}
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				if !assert.NoError(collect, cl.Get(ctx, client.ObjectKeyFromObject(cos), cos)) {
					return
				}
				assert.False(collect, cos.Status.CompletedAt.IsZero(), "completedAt should be set after rollout; conditions: %v", cos.Status.Conditions)
				ready := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
				if assert.NotNil(collect, ready) {
					assert.Equal(collect, metav1.ConditionTrue, ready.Status)
					assert.Equal(collect, ocv1.ClusterObjectSetReasonAllObjectsReady, ready.Reason)
				}
			}, rolloutTimeout, 100*time.Millisecond)
			cm := &corev1.ConfigMap{}
			require.NoError(t, cl.Get(ctx, client.ObjectKey{Name: name, Namespace: ns.Name}, cm))
			require.Equal(t, "world", cm.Data["hello"])
			require.NotNil(t, metav1.GetControllerOf(cm))
			require.Equal(t, cos.UID, metav1.GetControllerOf(cm).UID)

			if secret != nil {
				// A completed COS does not poll. Replacing an owned source Secret
				// must trigger content verification, and restoring it must unblock
				// reconciliation without changing the COS or its managed objects.
				original := secret.DeepCopy()
				require.NoError(t, cl.Delete(ctx, secret))
				secret.ResourceVersion = ""
				secret.UID = ""
				changed := manifest.DeepCopy()
				changed.Object["data"] = map[string]any{"hello": "changed"}
				secret.Data["object"], err = json.Marshal(changed.Object)
				require.NoError(t, err)
				require.NoError(t, cl.Create(ctx, secret))
				require.EventuallyWithT(t, func(collect *assert.CollectT) {
					if !assert.NoError(collect, cl.Get(ctx, client.ObjectKeyFromObject(cos), cos)) {
						return
					}
					condition := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
					if assert.NotNil(collect, condition) {
						assert.Equal(collect, ocv1.ClusterObjectSetReasonBlocked, condition.Reason)
						assert.Contains(collect, condition.Message, "resolved content of 1 phase(s) has changed")
					}
				}, 30*time.Second, 100*time.Millisecond)

				require.NoError(t, cl.Delete(ctx, secret))
				original.ResourceVersion = ""
				original.UID = ""
				require.NoError(t, cl.Create(ctx, original))
				require.EventuallyWithT(t, func(collect *assert.CollectT) {
					if !assert.NoError(collect, cl.Get(ctx, client.ObjectKeyFromObject(cos), cos)) {
						return
					}
					condition := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
					if assert.NotNil(collect, condition) {
						assert.Equal(collect, ocv1.ClusterObjectSetReasonAllObjectsReady, condition.Reason)
					}
				}, 30*time.Second, 100*time.Millisecond)
			}

			// Observe managed-object changes without updating the ClusterObjectSet.
			originalUID := cm.UID
			require.NoError(t, cl.Delete(ctx, cm))
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				if !assert.NoError(collect, cl.Get(ctx, client.ObjectKeyFromObject(cm), cm)) {
					return
				}
				assert.NotEqual(collect, originalUID, cm.UID)
				assert.Equal(collect, "world", cm.Data["hello"])
				if assert.NotNil(collect, metav1.GetControllerOf(cm)) {
					assert.Equal(collect, cos.UID, metav1.GetControllerOf(cm).UID)
				}
			}, time.Minute, 100*time.Millisecond)

			// The controller releases its finalizer independently of ClusterExtension.
			// The owner reference above lets Kubernetes garbage-collect the ConfigMap;
			// envtest does not run that garbage collector.
			require.NoError(t, cl.Delete(ctx, cos))
			require.Eventually(t, func() bool {
				return apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(cos), cos))
			}, time.Minute, 100*time.Millisecond)
		})
	}
}
