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

// Exercise the actual manager and revision engine without ClusterExtension or ClusterCatalog CRDs.
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

	waitForStableRevision := func(t *testing.T, cos *ocv1.ClusterObjectSet) {
		t.Helper()
		lastVersion, unchangedSince := cos.ResourceVersion, time.Now()
		require.Eventually(t, func() bool {
			if err := cl.Get(ctx, client.ObjectKeyFromObject(cos), cos); err != nil {
				return false
			}
			if cos.ResourceVersion != lastVersion {
				lastVersion, unchangedSince = cos.ResourceVersion, time.Now()
			}
			return time.Since(unchangedSince) >= time.Second
		}, 5*time.Second, 100*time.Millisecond)
	}

	for _, name := range []string{"inline", "secret-ref", "ce-owned-secret-ref", "ownerless-secret-ref", "mutable-secret-ref"} {
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
				ObjectMeta: metav1.ObjectMeta{Name: name + "-1"},
				Spec: ocv1.ClusterObjectSetSpec{
					Group:          name,
					LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive, Revision: 1,
					CollisionProtection: ocv1.CollisionProtectionPrevent,
					Phases:              []ocv1.ClusterObjectSetPhase{{Name: "deploy", Objects: []ocv1.ClusterObjectSetObject{obj}}},
				},
			}
			var deployment *ocv1.ClusterObjectDeployment
			if name == "ce-owned-secret-ref" {
				// Follow the production CE -> COD -> COS path: the deployment
				// creates the revision, while its source remains owned by CE.
				deployment = &ocv1.ClusterObjectDeployment{
					ObjectMeta: metav1.ObjectMeta{Name: name},
					Spec: ocv1.ClusterObjectDeploymentSpec{
						Template: ocv1.ClusterObjectDeploymentTemplate{
							Spec: ocv1.ClusterObjectDeploymentTemplateSpec{
								Phases: cos.Spec.Phases, CollisionProtection: cos.Spec.CollisionProtection,
							},
						},
					},
				}
				require.NoError(t, cl.Create(ctx, deployment))
			} else {
				require.NoError(t, cl.Create(ctx, cos))
			}
			if secret != nil && name != "ownerless-secret-ref" {
				if name == "ce-owned-secret-ref" || name == "mutable-secret-ref" {
					// Packed sources are owned by ClusterExtension, which is not
					// installed in this standalone manager's API server.
					secret.OwnerReferences = []metav1.OwnerReference{{
						APIVersion: ocv1.GroupVersion.String(), Kind: "ClusterExtension",
						Name: "test-extension", UID: "test-extension", Controller: ptr.To(true),
					}}
				} else {
					require.NoError(t, controllerutil.SetControllerReference(cos, secret, scheme.Scheme))
				}
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
						assert.Equal(collect, ocv1.ClusterObjectSetReasonPreconditionsNotMet, condition.Reason)
						assert.Contains(collect, condition.Message, "must be immutable but are not")
					}
				}, 5*time.Second, 100*time.Millisecond)

				// Let status-triggered reconciliations settle before changing only
				// the Secret. Recovery must precede the 10-second polling retry.
				waitForStableRevision(t, cos)
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
			if deployment != nil {
				require.NotNil(t, metav1.GetControllerOf(cos))
				require.Equal(t, deployment.UID, metav1.GetControllerOf(cos).UID)
			}

			if secret != nil {
				// Let initial status-triggered reconciliations finish before changing
				// only the source. A completed COS does not poll.
				waitForStableRevision(t, cos)

				// Replacing a source Secret must trigger content verification,
				// and restoring it must unblock
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
						assert.Equal(collect, ocv1.ClusterObjectSetReasonPreconditionsNotMet, condition.Reason)
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
			if deployment != nil {
				require.NoError(t, cl.Delete(ctx, deployment))
			}
			require.NoError(t, cl.Delete(ctx, cos))
			require.Eventually(t, func() bool {
				return apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(cos), cos))
			}, time.Minute, 100*time.Millisecond)
		})
	}

	t.Run("shared-secret-ref", func(t *testing.T) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "standalone-shared-"}}
		require.NoError(t, cl.Create(ctx, ns))
		manifest := func(name, value string) []byte {
			data, err := json.Marshal(&corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name},
				Data:       map[string]string{"hello": value},
			})
			require.NoError(t, err)
			return data
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "content", Namespace: ns.Name},
			Immutable:  ptr.To(true),
			Data: map[string][]byte{
				"first":  manifest("shared-first", "world"),
				"second": manifest("shared-second", "world"),
			},
		}
		require.NoError(t, cl.Create(ctx, secret))
		revisions := make([]*ocv1.ClusterObjectSet, 0, 2)
		for _, key := range []string{"first", "second"} {
			cos := &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Name: "shared-" + key + "-1"},
				Spec: ocv1.ClusterObjectSetSpec{
					Group: "shared-" + key, Revision: 1,
					LifecycleState:      ocv1.ClusterObjectSetLifecycleStateActive,
					CollisionProtection: ocv1.CollisionProtectionPrevent,
					Phases: []ocv1.ClusterObjectSetPhase{{Name: "deploy", Objects: []ocv1.ClusterObjectSetObject{{
						Ref: ocv1.ObjectSourceRef{Namespace: secret.Namespace, Name: secret.Name, Key: key},
					}}}},
				},
			}
			require.NoError(t, cl.Create(ctx, cos))
			revisions = append(revisions, cos)
		}
		waitForReason := func(reason string, timeout time.Duration) {
			t.Helper()
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				for _, cos := range revisions {
					if !assert.NoError(collect, cl.Get(ctx, client.ObjectKeyFromObject(cos), cos)) {
						continue
					}
					condition := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
					if assert.NotNil(collect, condition) {
						assert.Equal(collect, reason, condition.Reason, "revision %s", cos.Name)
					}
				}
			}, timeout, 100*time.Millisecond)
		}
		waitForReason(ocv1.ClusterObjectSetReasonAllObjectsReady, time.Minute)
		for _, cos := range revisions {
			waitForStableRevision(t, cos)
		}

		changed := secret.DeepCopy()
		changed.ResourceVersion = ""
		changed.UID = ""
		changed.Data["first"] = manifest("shared-first", "changed")
		changed.Data["second"] = manifest("shared-second", "changed")
		require.NoError(t, cl.Delete(ctx, secret))
		require.NoError(t, cl.Create(ctx, changed))
		waitForReason(ocv1.ClusterObjectSetReasonPreconditionsNotMet, 5*time.Second)

		require.NoError(t, cl.Delete(ctx, changed))
		secret.ResourceVersion = ""
		secret.UID = ""
		require.NoError(t, cl.Create(ctx, secret))
		waitForReason(ocv1.ClusterObjectSetReasonAllObjectsReady, 5*time.Second)
	})
}
