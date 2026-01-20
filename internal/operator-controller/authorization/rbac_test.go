package authorization

import (
	"context"
	"errors"
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
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/kubernetes/pkg/registry/rbac/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

	saName   = "test-serviceaccount"
	ns       = "test-namespace"
	testUser = &user.DefaultInfo{Name: fmt.Sprintf("system:serviceaccount:%s:%s", ns, saName)}

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
				Resources: []string{"serviceaccounts", "services"},
				Verbs:     []string{"*"},
			},
			{
				APIGroups: []string{"rbac.authorization.k8s.io"},
				Resources: []string{"roles", "clusterroles", "rolebindings", "clusterrolebindings"},
				Verbs:     []string{"get", "patch", "watch", "list", "create", "update", "delete", "escalate", "bind"},
			},
		},
	}

	expectedSingleNamespaceMissingRules = []ScopedPolicyRules{
		{
			Namespace: "",
			MissingRules: []rbacv1.PolicyRule{
				{
					Verbs:           []string{"list", "watch"},
					APIGroups:       []string{""},
					Resources:       []string{"services"},
					ResourceNames:   []string(nil),
					NonResourceURLs: []string(nil)},
				{
					Verbs:           []string{"list", "watch"},
					APIGroups:       []string{"rbac.authorization.k8s.io"},
					Resources:       []string{"rolebindings"},
					ResourceNames:   []string(nil),
					NonResourceURLs: []string(nil)},
				{
					Verbs:           []string{"list", "watch"},
					APIGroups:       []string{"rbac.authorization.k8s.io"},
					Resources:       []string{"roles"},
					ResourceNames:   []string(nil),
					NonResourceURLs: []string(nil),
				},
			},
		},
		{
			Namespace: "test-namespace",
			MissingRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"*"},
					Resources: []string{"certificates"}},
				{
					Verbs:     []string{"create"},
					APIGroups: []string{""},
					Resources: []string{"services"}},
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"rolebindings"}},
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"roles"}},
				{
					Verbs:         []string{"delete", "get", "patch", "update"},
					APIGroups:     []string{""},
					Resources:     []string{"services"},
					ResourceNames: []string{"test-service"}},
				{
					Verbs:         []string{"delete", "get", "patch", "update"},
					APIGroups:     []string{"rbac.authorization.k8s.io"},
					Resources:     []string{"rolebindings"},
					ResourceNames: []string{"test-extension-binding"}},
				{
					Verbs:         []string{"delete", "get", "patch", "update"},
					APIGroups:     []string{"rbac.authorization.k8s.io"},
					Resources:     []string{"roles"},
					ResourceNames: []string{"test-extension-role"}},
				{
					Verbs:     []string{"watch"},
					APIGroups: []string{"*"},
					Resources: []string{"serviceaccounts"},
				},
			},
		},
	}

	expectedMultiNamespaceMissingRules = []ScopedPolicyRules{
		{
			Namespace: "",
			MissingRules: []rbacv1.PolicyRule{
				{
					Verbs:           []string{"list", "watch"},
					APIGroups:       []string{""},
					Resources:       []string{"services"},
					ResourceNames:   []string(nil),
					NonResourceURLs: []string(nil)},
				{
					Verbs:           []string{"list", "watch"},
					APIGroups:       []string{"rbac.authorization.k8s.io"},
					Resources:       []string{"rolebindings"},
					ResourceNames:   []string(nil),
					NonResourceURLs: []string(nil)},
				{
					Verbs:           []string{"list", "watch"},
					APIGroups:       []string{"rbac.authorization.k8s.io"},
					Resources:       []string{"roles"},
					ResourceNames:   []string(nil),
					NonResourceURLs: []string(nil),
				},
			},
		},
		{
			Namespace: "a-test-namespace",
			MissingRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"*"},
					Resources: []string{"certificates"}},
				{
					Verbs:     []string{"create"},
					APIGroups: []string{""},
					Resources: []string{"services"}},
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"rolebindings"}},
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"roles"}},
				{
					Verbs:         []string{"delete", "get", "patch", "update"},
					APIGroups:     []string{""},
					Resources:     []string{"services"},
					ResourceNames: []string{"test-service"}},
				{
					Verbs:         []string{"delete", "get", "patch", "update"},
					APIGroups:     []string{"rbac.authorization.k8s.io"},
					Resources:     []string{"rolebindings"},
					ResourceNames: []string{"test-extension-binding"}},
				{
					Verbs:         []string{"delete", "get", "patch", "update"},
					APIGroups:     []string{"rbac.authorization.k8s.io"},
					Resources:     []string{"roles"},
					ResourceNames: []string{"test-extension-role"}},
				{
					Verbs:     []string{"watch"},
					APIGroups: []string{"*"},
					Resources: []string{"serviceaccounts"},
				},
			},
		},
		{
			Namespace: "test-namespace",
			MissingRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"*"},
					Resources: []string{"certificates"}},
				{
					Verbs:     []string{"create"},
					APIGroups: []string{""},
					Resources: []string{"services"}},
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"rolebindings"}},
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"roles"}},
				{
					Verbs:         []string{"delete", "get", "patch", "update"},
					APIGroups:     []string{""},
					Resources:     []string{"services"},
					ResourceNames: []string{"test-service"}},
				{
					Verbs:         []string{"delete", "get", "patch", "update"},
					APIGroups:     []string{"rbac.authorization.k8s.io"},
					Resources:     []string{"rolebindings"},
					ResourceNames: []string{"test-extension-binding"}},
				{
					Verbs:         []string{"delete", "get", "patch", "update"},
					APIGroups:     []string{"rbac.authorization.k8s.io"},
					Resources:     []string{"roles"},
					ResourceNames: []string{"test-extension-role"}},
				{
					Verbs:     []string{"watch"},
					APIGroups: []string{"*"},
					Resources: []string{"serviceaccounts"},
				},
			},
		},
	}
)

func setupFakeClient(role client.Object) client.Client {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = rbacv1.AddToScheme(s)
	restMapper := testrestmapper.TestOnlyStaticRESTMapper(s)
	fakeClientBuilder := fake.NewClientBuilder().WithObjects(append(objects, role)...).WithRESTMapper(restMapper)
	return fakeClientBuilder.Build()
}

func TestPreAuthorize_Success(t *testing.T) {
	t.Run("preauthorize succeeds with no missing rbac rules", func(t *testing.T) {
		fakeClient := setupFakeClient(privilegedClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		missingRules, err := preAuth.PreAuthorize(context.TODO(), testUser, strings.NewReader(testManifest))
		require.NoError(t, err)
		require.Equal(t, []ScopedPolicyRules{}, missingRules)
	})
}

func TestPreAuthorize_MissingRBAC(t *testing.T) {
	t.Run("preauthorize fails and finds missing rbac rules", func(t *testing.T) {
		fakeClient := setupFakeClient(limitedClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		missingRules, err := preAuth.PreAuthorize(context.TODO(), testUser, strings.NewReader(testManifest))
		require.NoError(t, err)
		require.Equal(t, expectedSingleNamespaceMissingRules, missingRules)
	})
}

func TestPreAuthorizeMultiNamespace_MissingRBAC(t *testing.T) {
	t.Run("preauthorize fails and finds missing rbac rules in multiple namespaces", func(t *testing.T) {
		fakeClient := setupFakeClient(limitedClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		missingRules, err := preAuth.PreAuthorize(context.TODO(), testUser, strings.NewReader(testManifestMultiNamespace))
		require.NoError(t, err)
		require.Equal(t, expectedMultiNamespaceMissingRules, missingRules)
	})
}

func TestPreAuthorize_CheckEscalation(t *testing.T) {
	t.Run("preauthorize succeeds with no missing rbac rules", func(t *testing.T) {
		fakeClient := setupFakeClient(escalatingClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		missingRules, err := preAuth.PreAuthorize(context.TODO(), testUser, strings.NewReader(testManifest))
		require.NoError(t, err)
		require.Equal(t, []ScopedPolicyRules{}, missingRules)
	})
}

func TestPreAuthorize_AdditionalRequiredPerms_MissingRBAC(t *testing.T) {
	t.Run("preauthorize fails and finds missing rbac rules coming from the additional required permissions", func(t *testing.T) {
		fakeClient := setupFakeClient(escalatingClusterRole)
		preAuth := NewRBACPreAuthorizer(fakeClient)
		missingRules, err := preAuth.PreAuthorize(context.TODO(), testUser, strings.NewReader(testManifest), func(user user.Info) []authorizer.AttributesRecord {
			return []authorizer.AttributesRecord{
				{
					User:            user,
					Verb:            "create",
					APIGroup:        corev1.SchemeGroupVersion.Group,
					APIVersion:      corev1.SchemeGroupVersion.Version,
					Resource:        "pods",
					ResourceRequest: true,
				},
			}
		})
		require.NoError(t, err)
		require.Equal(t, []ScopedPolicyRules{
			{
				Namespace: "",
				MissingRules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"create"},
						APIGroups: []string{""},
						Resources: []string{"pods"},
					},
				},
			},
		}, missingRules)
	})
}

// TestParseEscalationErrorForMissingRules Are tests with respect to https://github.com/kubernetes/api/blob/e8d4d542f6a9a16a694bfc8e3b8cd1557eecfc9d/rbac/v1/types.go#L49-L74
// Goal is: prove the regex works as planned AND that if the error messages ever change we'll learn about it with these tests
func TestParseEscalationErrorForMissingRules_ParsingLogic(t *testing.T) {
	testCases := []struct {
		name           string
		inputError     error
		expectedResult *parseResult
		expectError    require.ErrorAssertionFunc
	}{
		{
			name: "One Missing Resource Rule",
			inputError: errors.New(`user "test-user" (groups=["test"]) is attempting to grant RBAC permissions not currently held:
{APIGroups:["apps"], Resources:["deployments"], Verbs:["get"]}`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"get"}},
				},
			},
			expectError: require.NoError,
		},
		{
			name: "Multiple Missing Rules (Resource + NonResource)",
			inputError: errors.New(`user "sa" (groups=["system:authenticated"]) is attempting to grant RBAC permissions not currently held:
{APIGroups:[""], Resources:["pods"], Verbs:["list" "watch"]}
{NonResourceURLs:["/healthz"], Verbs:["get"]}`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list", "watch"}},
					{NonResourceURLs: []string{"/healthz"}, Verbs: []string{"get"}},
				},
			},
			expectError: require.NoError,
		},
		{
			name: "One Missing Rule with Resolution Errors",
			inputError: errors.New(`user "test-admin" (groups=["system:masters"]) is attempting to grant RBAC permissions not currently held:
{APIGroups:["batch"], Resources:["jobs"], Verbs:["create"]}; resolution errors: [role "missing-role" not found]`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{"batch"}, Resources: []string{"jobs"}, Verbs: []string{"create"}},
				},
				ResolutionErrors: errors.New(`[role "missing-role" not found]`),
			},
			expectError: require.NoError,
		},
		{
			name: "Multiple Missing Rules with Resolution Errors",
			inputError: errors.New(`user "another-user" (groups=[]) is attempting to grant RBAC permissions not currently held:
{APIGroups:[""], Resources:["secrets"], Verbs:["get"]}
{APIGroups:[""], Resources:["configmaps"], Verbs:["list"]}; resolution errors: [clusterrole "missing-clusterrole" not found, role "other-missing" not found]`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}},
					{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"list"}},
				},
				ResolutionErrors: errors.New(`[clusterrole "missing-clusterrole" not found, role "other-missing" not found]`),
			},
			expectError: require.NoError,
		},
		{
			name: "Missing Rule (All Resource Fields)",
			inputError: errors.New(`user "resource-name-user" (groups=["test"]) is attempting to grant RBAC permissions not currently held:
{APIGroups:["extensions"], Resources:["ingresses"], ResourceNames:["my-ingress"], Verbs:["update" "patch"]}`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{"extensions"}, Resources: []string{"ingresses"}, ResourceNames: []string{"my-ingress"}, Verbs: []string{"update", "patch"}},
				},
			},
			expectError: require.NoError,
		},
		{
			name: "Missing Rule (No ResourceNames)",
			inputError: errors.New(`user "no-res-name-user" (groups=["test"]) is attempting to grant RBAC permissions not currently held:
{APIGroups:["networking.k8s.io"], Resources:["networkpolicies"], Verbs:["watch"]}`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies"}, Verbs: []string{"watch"}},
				},
			},
			expectError: require.NoError,
		},
		{
			name: "Missing Rule (NonResourceURLs only)",
			inputError: errors.New(`user "url-user" (groups=["test"]) is attempting to grant RBAC permissions not currently held:
{NonResourceURLs:["/version" "/apis"], Verbs:["get"]}`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{NonResourceURLs: []string{"/version", "/apis"}, Verbs: []string{"get"}},
				},
			},
			expectError: require.NoError,
		},
		{
			name:           "Unexpected Format",
			inputError:     errors.New("some completely different error message that doesn't match"),
			expectedResult: &parseResult{},
			expectError: func(t require.TestingT, err error, i ...interface{}) {
				require.ErrorContains(t, err, "unexpected format of escalation check error string")
			},
		},
		{
			name: "Empty Permissions String",
			inputError: errors.New(`user "empty-perms" (groups=["test"]) is attempting to grant RBAC permissions not currently held:
`),
			expectedResult: &parseResult{},
			expectError: func(t require.TestingT, err error, i ...interface{}) {
				require.ErrorContains(t, err, "unexpected format of escalation check error string")
			},
		},
		{
			name: "Rule with Empty Strings in lists",
			inputError: errors.New(`user "empty-strings" (groups=["test"]) is attempting to grant RBAC permissions not currently held:
{APIGroups:["" "apps"], Resources:["" "deployments"], Verbs:["get" ""]}`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{"", "apps"}, Resources: []string{"", "deployments"}, Verbs: []string{"get", ""}},
				},
			},
			expectError: require.NoError,
		},
		{
			name: "Rule with Only Empty Verb",
			inputError: errors.New(`user "empty-verb" (groups=["test"]) is attempting to grant RBAC permissions not currently held:
{APIGroups:[""], Resources:["pods"], Verbs:[""]}`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{""}},
				},
			},
			expectError: require.NoError,
		},
		{
			name: "Rule with no fields",
			inputError: errors.New(`user "empty-verb" (groups=["test"]) is attempting to grant RBAC permissions not currently held:
{}`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{{}},
			},
			expectError: require.NoError,
		},
		{
			name: "Rule with no colon separator",
			inputError: errors.New(`user "empty-verb" (groups=["test"]) is attempting to grant RBAC permissions not currently held:
{APIGroups:[""], Resources, Verbs:["get"]}
`),
			expectedResult: &parseResult{},
			expectError: func(t require.TestingT, err error, i ...interface{}) {
				require.ErrorContains(t, err, `unexpected item "Resources": expected <Type>:[<values>...]`)
			},
		},
		{
			name: "Rule with unknown field",
			inputError: errors.New(`user "empty-verb" (groups=["test"]) is attempting to grant RBAC permissions not currently held:
{FooBar:["baz"]}
{APIGroups:[""], Resources:["secrets"], Verbs:["get"]}
`),
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}},
				},
			},
			expectError: func(t require.TestingT, err error, i ...interface{}) {
				require.ErrorContains(t, err, `unknown field: "FooBar"`)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := parseEscalationErrorForMissingRules(tc.inputError)
			tc.expectError(t, err)
			require.Equal(t, tc.expectedResult, rules)
		})
	}
}

func TestParseEscalationErrorForMissingRules_KubernetesCompatibility(t *testing.T) {
	testCases := []struct {
		name                string
		ruleResolver        validation.AuthorizationRuleResolver
		wantRules           []rbacv1.PolicyRule
		expectedErrorString string
		expectedResult      *parseResult
	}{
		{
			name: "missing rules",
			ruleResolver: mockRulesResolver{
				rules: []rbacv1.PolicyRule{},
				err:   nil,
			},
			wantRules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}, ResourceNames: []string{"test-secret"}},
				{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list", "watch"}},
				{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets"}, Verbs: []string{"create", "update", "patch", "delete"}},
				{NonResourceURLs: []string{"/healthz", "/livez"}, Verbs: []string{"get", "post"}},
			},
			expectedErrorString: `user "user" (groups=["a" "b"]) is attempting to grant RBAC permissions not currently held:
{APIGroups:[""], Resources:["configmaps"], Verbs:["get" "list" "watch"]}
{APIGroups:[""], Resources:["secrets"], ResourceNames:["test-secret"], Verbs:["get"]}
{APIGroups:["apps"], Resources:["deployments"], Verbs:["create" "update" "patch" "delete"]}
{APIGroups:["apps"], Resources:["replicasets"], Verbs:["create" "update" "patch" "delete"]}
{NonResourceURLs:["/healthz"], Verbs:["get"]}
{NonResourceURLs:["/healthz"], Verbs:["post"]}
{NonResourceURLs:["/livez"], Verbs:["get"]}
{NonResourceURLs:["/livez"], Verbs:["post"]}`,
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list", "watch"}},
					{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}, ResourceNames: []string{"test-secret"}},
					{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"create", "update", "patch", "delete"}},
					{APIGroups: []string{"apps"}, Resources: []string{"replicasets"}, Verbs: []string{"create", "update", "patch", "delete"}},
					{NonResourceURLs: []string{"/healthz"}, Verbs: []string{"get"}},
					{NonResourceURLs: []string{"/healthz"}, Verbs: []string{"post"}},
					{NonResourceURLs: []string{"/livez"}, Verbs: []string{"get"}},
					{NonResourceURLs: []string{"/livez"}, Verbs: []string{"post"}},
				},
			},
		},
		{
			name: "resolution failure",
			ruleResolver: mockRulesResolver{
				rules: []rbacv1.PolicyRule{},
				err:   errors.New("resolution error"),
			},
			wantRules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}, ResourceNames: []string{"test-secret"}},
				{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list", "watch"}},
				{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets"}, Verbs: []string{"create", "update", "patch", "delete"}},
				{NonResourceURLs: []string{"/healthz", "/livez"}, Verbs: []string{"get", "post"}},
			},
			expectedErrorString: `user "user" (groups=["a" "b"]) is attempting to grant RBAC permissions not currently held:
{APIGroups:[""], Resources:["configmaps"], Verbs:["get" "list" "watch"]}
{APIGroups:[""], Resources:["secrets"], ResourceNames:["test-secret"], Verbs:["get"]}
{APIGroups:["apps"], Resources:["deployments"], Verbs:["create" "update" "patch" "delete"]}
{APIGroups:["apps"], Resources:["replicasets"], Verbs:["create" "update" "patch" "delete"]}
{NonResourceURLs:["/healthz"], Verbs:["get"]}
{NonResourceURLs:["/healthz"], Verbs:["post"]}
{NonResourceURLs:["/livez"], Verbs:["get"]}
{NonResourceURLs:["/livez"], Verbs:["post"]}; resolution errors: [resolution error]`,
			expectedResult: &parseResult{
				MissingRules: []rbacv1.PolicyRule{
					{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list", "watch"}},
					{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}, ResourceNames: []string{"test-secret"}},
					{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"create", "update", "patch", "delete"}},
					{APIGroups: []string{"apps"}, Resources: []string{"replicasets"}, Verbs: []string{"create", "update", "patch", "delete"}},
					{NonResourceURLs: []string{"/healthz"}, Verbs: []string{"get"}},
					{NonResourceURLs: []string{"/healthz"}, Verbs: []string{"post"}},
					{NonResourceURLs: []string{"/livez"}, Verbs: []string{"get"}},
					{NonResourceURLs: []string{"/livez"}, Verbs: []string{"post"}},
				},
				ResolutionErrors: errors.New("[resolution error]"),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := request.WithUser(request.WithNamespace(context.Background(), "namespace"), &user.DefaultInfo{
				Name:   "user",
				Groups: []string{"a", "b"},
			})

			// Let's actually call the upstream function that generates and returns the
			// error message that we are attempting to parse correctly. The hope is that
			// these tests will start failing if we bump to a new version of kubernetes
			// that causes our parsing logic to be incorrect.
			err := validation.ConfirmNoEscalation(ctx, tc.ruleResolver, tc.wantRules)
			require.Error(t, err)
			require.Equal(t, tc.expectedErrorString, err.Error())

			res, err := parseEscalationErrorForMissingRules(err)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResult, res)
		})
	}
}

type mockRulesResolver struct {
	rules []rbacv1.PolicyRule
	err   error
}

func (m mockRulesResolver) GetRoleReferenceRules(ctx context.Context, roleRef rbacv1.RoleRef, namespace string) ([]rbacv1.PolicyRule, error) {
	panic("unimplemented")
}

func (m mockRulesResolver) RulesFor(ctx context.Context, user user.Info, namespace string) ([]rbacv1.PolicyRule, error) {
	return m.rules, m.err
}

func (m mockRulesResolver) VisitRulesFor(ctx context.Context, user user.Info, namespace string, visitor func(source fmt.Stringer, rule *rbacv1.PolicyRule, err error) bool) {
	panic("unimplemented")
}
