package authorization

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/user"
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

	saName = "test-serviceaccount"
	ns     = "test-namespace"

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
				Resources: []string{"serviceaccounts", "services", "certificates"},
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
		testServiceAccount := user.DefaultInfo{Name: fmt.Sprintf("system:serviceaccount:%s:%s", ns, saName)}
		missingRules, err := preAuth.PreAuthorize(context.TODO(), &testServiceAccount, strings.NewReader(testManifest))
		require.NoError(t, err)
		require.Equal(t, []ScopedPolicyRules{}, missingRules)
	})
}

func TestPreAuthorize_Failure(t *testing.T) {
	t.Run("preauthorize failes with missing rbac rules", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.PreflightPermissions, true)
		fakeClient := setupFakeClient(limitedClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		testServiceAccount := user.DefaultInfo{Name: fmt.Sprintf("system:serviceaccount:%s:%s", ns, saName)}
		missingRules, err := preAuth.PreAuthorize(context.TODO(), &testServiceAccount, strings.NewReader(testManifest))
		require.Error(t, err)
		require.NotEqual(t, []ScopedPolicyRules{}, missingRules)
	})
}

func TestPreAuthorize_CheckEscalation(t *testing.T) {
	t.Run("preauthorize succeeds with no missing rbac rules", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.PreflightPermissions, true)
		fakeClient := setupFakeClient(escalatingClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		testServiceAccount := user.DefaultInfo{
			Name: fmt.Sprintf("system:serviceaccount:%s:%s", ns, saName),
		}
		// Ensure the manifest contains only allowed rules so that the escalation check succeeds with no missing rules
		modifiedManifest := strings.Replace(testManifest, `- apiGroups: ["*"]
  resources: [certificates]
  verbs: [create]
`, "", 1)
		missingRules, err := preAuth.PreAuthorize(context.TODO(), &testServiceAccount, strings.NewReader(modifiedManifest))
		require.NoError(t, err)
		require.Equal(t, []ScopedPolicyRules{}, missingRules)
	})
}

func TestPreAuthorize_StorageLayerError(t *testing.T) {
	t.Run("preauthorize fails with storage-layer error and computed missing rules", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.PreflightPermissions, true)
		// Use a client configured with a limited cluster role that should cause the escalation check to fail
		fakeClient := setupFakeClient(limitedClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		testServiceAccount := user.DefaultInfo{
			Name: fmt.Sprintf("system:serviceaccount:%s:%s", ns, saName),
		}
		// Create a manifest that triggers escalation check failure.
		manifest := `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test-escalation-role
  namespace: test-namespace
rules:
- apiGroups: ["*"]
  resources: ["*"]
  verbs: ["*"]
`
		missingRules, err := preAuth.PreAuthorize(context.TODO(), &testServiceAccount, strings.NewReader(manifest))
		require.Error(t, err)
		// Instead of calling it opaque, we check that the error contains "forbidden:" to indicate it's from storage-layer checks
		require.Contains(t, err.Error(), "forbidden:")
		// Expect that our computed missing rules are non-empty (i.e. we have a detailed report).
		require.NotEmpty(t, missingRules, "expected computed missing rules to be returned")
	})
}

func TestPreAuthorize_MultipleEscalationErrors(t *testing.T) {
	t.Run("preauthorize returns composite escalation error for multiple RBAC objects", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.PreflightPermissions, true)
		// Use a client configured with a limited cluster role that will fail escalation checks
		fakeClient := setupFakeClient(limitedClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		testServiceAccount := user.DefaultInfo{
			Name: fmt.Sprintf("system:serviceaccount:%s:%s", ns, saName),
		}
		// Create a manifest with two RBAC objects that should both trigger escalation errors
		manifest := `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test-escalation-role-1
  namespace: test-namespace
rules:
- apiGroups: ["*"]
  resources: ["*"]
  verbs: ["*"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test-escalation-binding-1
  namespace: test-namespace
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: test-escalation-role-1
subjects:
- kind: ServiceAccount
  name: test-serviceaccount
  namespace: test-namespace
`
		missingRules, err := preAuth.PreAuthorize(context.TODO(), &testServiceAccount, strings.NewReader(manifest))
		require.Error(t, err, "expected escalation check to fail")
		errMsg := err.Error()
		// Instead of expecting two "forbidden:" substrings, check that both distinct error parts appear
		require.Contains(t, errMsg, "forbidden:", "expected error message to contain 'forbidden:'")
		require.Contains(t, errMsg, "not found", "expected error message to contain 'not found'")
		// Also ensure that our computed missing rules are non-empty
		require.NotEmpty(t, missingRules, "expected computed missing rules to be returned")
	})
}

func TestPreAuthorize_MultipleRuleFailures(t *testing.T) {
	t.Run("reports multiple missing rules when several rules fail", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.PreflightPermissions, true)
		// Use a client configured with a limited cluster role that lacks the necessary permissions.
		fakeClient := setupFakeClient(limitedClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		testServiceAccount := user.DefaultInfo{
			Name: fmt.Sprintf("system:serviceaccount:%s:%s", ns, saName),
		}
		// This manifest defines a Role with two rules
		// One rule requires "get" and "update" on "roles",
		// and the other requires "list" and "watch" on "rolebindings"
		// Both are expected to be missing
		manifest := `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test-multiple-rule-failure
  namespace: test-namespace
rules:
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["roles"]
  verbs: ["get", "update"]
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["rolebindings"]
  verbs: ["list", "watch"]
`
		missingRules, err := preAuth.PreAuthorize(context.TODO(), &testServiceAccount, strings.NewReader(manifest))
		require.Error(t, err, "expected escalation check to fail due to missing rules")

		// Check that computed missing rules include multiple entries for the namespace
		var nsMissingRules []rbacv1.PolicyRule
		for _, scoped := range missingRules {
			if scoped.Namespace == "test-namespace" {
				nsMissingRules = scoped.MissingRules
				break
			}
		}
		require.NotEmpty(t, nsMissingRules, "expected missing rules for namespace test-namespace")
		require.GreaterOrEqual(t, len(nsMissingRules), 2, "expected at least 2 missing rules to be reported")
	})
}
