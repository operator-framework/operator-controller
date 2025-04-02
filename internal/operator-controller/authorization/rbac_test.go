package authorization

import (
	"context"
	"strings"
	"testing"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

var (
	testManifest = `apiVersion: v1
kind: Service
metadata:
  name: test-service
  namespace: test-namespace
spec:
  clusterIP: None
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test-extension-role
  namespace: test-namespace
rules:
- apiGroups: ["*"]
  resources: [serviceaccounts]
  verbs: [watch]
- apiGroups: ["*"]
  resources: [certificates]
  verbs: [create]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test-extension-binding
  namespace: test-namespace
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: test-extension-role
subjects:
- kind: ServiceAccount
  name: test-serviceaccount
  namespace: test-namespace
  `

	testManifestMultiNamespace = `apiVersion: v1
kind: Service
metadata:
  name: test-service
  namespace: test-namespace
spec:
  clusterIP: None
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test-extension-role
  namespace: test-namespace
rules:
- apiGroups: ["*"]
  resources: [serviceaccounts]
  verbs: [watch]
- apiGroups: ["*"]
  resources: [certificates]
  verbs: [create]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test-extension-binding
  namespace: test-namespace
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: test-extension-role
subjects:
- kind: ServiceAccount
  name: test-serviceaccount
  namespace: test-namespace
---
kind: Service
metadata:
  name: test-service
  namespace: a-test-namespace
spec:
  clusterIP: None
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test-extension-role
  namespace: a-test-namespace
rules:
- apiGroups: ["*"]
  resources: [serviceaccounts]
  verbs: [watch]
- apiGroups: ["*"]
  resources: [certificates]
  verbs: [create]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test-extension-binding
  namespace: a-test-namespace
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: test-extension-role
subjects:
- kind: ServiceAccount
  name: test-serviceaccount
  namespace: a-test-namespace
  `

	saName                  = "test-serviceaccount"
	ns                      = "test-namespace"
	exampleClusterExtension = ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster-extension"},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: ns,
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: saName,
			},
		},
	}

	objects = []client.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace",
			},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "admin-clusterrole-binding",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      saName,
					Namespace: ns,
				},
			},
			RoleRef: rbacv1.RoleRef{
				Name: "admin-clusterrole",
				Kind: "ClusterRole",
			},
		},
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-serviceaccount",
				Namespace: "test-namespace",
			},
		},
	}

	privilegedClusterRole = &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "admin-clusterrole",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"*"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
		},
	}

	limitedClusterRole = &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "admin-clusterrole",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{""},
				Verbs:     []string{""},
			},
		},
	}

	escalatingClusterRole = &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "admin-clusterrole",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"*"},
				Resources: []string{"serviceaccounts", "services", "clusterextensions/finalizers"},
				Verbs:     []string{"*"},
			},
			{
				APIGroups: []string{"rbac.authorization.k8s.io"},
				Resources: []string{"roles", "clusterroles", "rolebindings", "clusterrolebindings"},
				Verbs:     []string{"get", "patch", "watch", "list", "create", "update", "delete", "escalate", "bind"},
			},
		},
	}
)

func setupFakeClient(role client.Object) client.Client {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = rbacv1.AddToScheme(s)
	restMapper := testrestmapper.TestOnlyStaticRESTMapper(s)
	// restMapper := meta.NewDefaultRESTMapper(nil)
	fakeClientBuilder := fake.NewClientBuilder().WithObjects(append(objects, role)...).WithRESTMapper(restMapper)
	return fakeClientBuilder.Build()
}

func TestPreAuthorize_Success(t *testing.T) {
	t.Run("preauthorize succeeds with no missing rbac rules", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.PreflightPermissions, true)
		fakeClient := setupFakeClient(privilegedClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		missingRules, err := preAuth.PreAuthorize(context.TODO(), &exampleClusterExtension, strings.NewReader(testManifest))
		require.NoError(t, err)
		require.Equal(t, []ScopedPolicyRules{}, missingRules)
	})
}

func TestPreAuthorize_Failure(t *testing.T) {
	t.Run("preauthorize fails with missing rbac rules", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.PreflightPermissions, true)
		fakeClient := setupFakeClient(limitedClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		missingRules, err := preAuth.PreAuthorize(context.TODO(), &exampleClusterExtension, strings.NewReader(testManifest))
		require.Error(t, err)
		require.NotEqual(t, []ScopedPolicyRules{}, missingRules)
	})
}

func TestPreAuthorizeMultiNamespace_Failure(t *testing.T) {
	t.Run("preauthorize fails with missing rbac rules in multiple namespaces", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.PreflightPermissions, true)
		fakeClient := setupFakeClient(limitedClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		missingRules, err := preAuth.PreAuthorize(context.TODO(), &exampleClusterExtension, strings.NewReader(testManifestMultiNamespace))
		require.Error(t, err)
		require.NotEqual(t, []ScopedPolicyRules{}, missingRules)
	})
}

func TestPreAuthorize_CheckEscalation(t *testing.T) {
	t.Run("preauthorize succeeds with no missing rbac rules", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.PreflightPermissions, true)
		fakeClient := setupFakeClient(escalatingClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		missingRules, err := preAuth.PreAuthorize(context.TODO(), &exampleClusterExtension, strings.NewReader(testManifest))
		require.NoError(t, err)
		require.Equal(t, []ScopedPolicyRules{}, missingRules)
	})
}
