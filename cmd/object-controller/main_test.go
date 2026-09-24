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
	testEnv.CRDDirectoryPaths = []string{"../../helm/olmv1/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterobjectsets.yaml"}
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

	for _, name := range []string{"inline", "secret-ref"} {
		t.Run(name, func(t *testing.T) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "standalone-"}}
			require.NoError(t, cl.Create(ctx, ns))
			manifest := unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1", "kind": "ConfigMap",
				"metadata": map[string]any{"name": name, "namespace": ns.Name},
				"data":     map[string]any{"hello": "world"},
			}}
			obj := ocv1.ClusterObjectSetObject{Object: manifest}
			if name == "secret-ref" {
				data, err := json.Marshal(manifest.Object)
				require.NoError(t, err)
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "content", Namespace: ns.Name},
					Immutable:  ptr.To(true), Data: map[string][]byte{"object": data},
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
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				if !assert.NoError(collect, cl.Get(ctx, client.ObjectKeyFromObject(cos), cos)) {
					return
				}
				assert.True(collect, meta.IsStatusConditionTrue(cos.Status.Conditions, ocv1.ClusterObjectSetTypeSucceeded), "%v", cos.Status.Conditions)
				progressing := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeProgressing)
				if assert.NotNil(collect, progressing) {
					assert.Equal(collect, "Revision 1 has rolled out.", progressing.Message)
				}
			}, time.Minute, 100*time.Millisecond)
			cm := &corev1.ConfigMap{}
			require.NoError(t, cl.Get(ctx, client.ObjectKey{Name: name, Namespace: ns.Name}, cm))
			require.Equal(t, "world", cm.Data["hello"])
			require.NotNil(t, metav1.GetControllerOf(cm))
			require.Equal(t, cos.UID, metav1.GetControllerOf(cm).UID)

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
