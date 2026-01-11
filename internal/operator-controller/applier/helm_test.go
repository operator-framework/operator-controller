package applier_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authorization"
	"github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager"
	cmcache "github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager/cache"
)

var _ contentmanager.Manager = (*mockManagedContentCacheManager)(nil)

type mockManagedContentCacheManager struct {
	err   error
	cache cmcache.Cache
}

func (m *mockManagedContentCacheManager) Get(_ context.Context, _ *ocv1.ClusterExtension) (cmcache.Cache, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.cache, nil
}

func (m *mockManagedContentCacheManager) Delete(_ *ocv1.ClusterExtension) error {
	return m.err
}

type mockManagedContentCache struct {
	err error
}

var _ cmcache.Cache = (*mockManagedContentCache)(nil)

func (m *mockManagedContentCache) Close() error {
	if m.err != nil {
		return m.err
	}
	return nil
}

func (m *mockManagedContentCache) Watch(_ context.Context, _ cmcache.Watcher, _ ...client.Object) error {
	if m.err != nil {
		return m.err
	}
	return nil
}

type mockPreflight struct {
	installErr error
	upgradeErr error
}

type mockPreAuthorizer struct {
	fn func(context.Context, user.Info, io.Reader, ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error)
}

func (p *mockPreAuthorizer) PreAuthorize(ctx context.Context, manifestManager user.Info, manifestReader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
	return p.fn(ctx, manifestManager, manifestReader, additionalRequiredPerms...)
}

func (mp *mockPreflight) Install(context.Context, []client.Object) error {
	return mp.installErr
}

func (mp *mockPreflight) Upgrade(context.Context, []client.Object) error {
	return mp.upgradeErr
}

type mockHelmReleaseToObjectsConverter struct {
}

func (mockHelmReleaseToObjectsConverter) GetObjectsFromRelease(*release.Release) ([]client.Object, error) {
	return nil, nil
}

type mockActionGetter struct {
	actionClientForErr error
	getClientErr       error
	historyErr         error
	installErr         error
	dryRunInstallErr   error
	upgradeErr         error
	dryRunUpgradeErr   error
	reconcileErr       error
	desiredRel         *release.Release
	currentRel         *release.Release
	history            []*release.Release
}

func (mag *mockActionGetter) ActionClientFor(ctx context.Context, obj client.Object) (helmclient.ActionInterface, error) {
	return mag, mag.actionClientForErr
}

func (mag *mockActionGetter) Get(name string, opts ...helmclient.GetOption) (*release.Release, error) {
	return mag.currentRel, mag.getClientErr
}

func (mag *mockActionGetter) History(name string, opts ...helmclient.HistoryOption) ([]*release.Release, error) {
	return mag.history, mag.historyErr
}

func (mag *mockActionGetter) Install(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...helmclient.InstallOption) (*release.Release, error) {
	i := action.Install{}
	for _, opt := range opts {
		if err := opt(&i); err != nil {
			return nil, err
		}
	}
	if i.DryRun {
		return mag.desiredRel, mag.dryRunInstallErr
	}
	return mag.desiredRel, mag.installErr
}

func (mag *mockActionGetter) Upgrade(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...helmclient.UpgradeOption) (*release.Release, error) {
	i := action.Upgrade{}
	for _, opt := range opts {
		if err := opt(&i); err != nil {
			return nil, err
		}
	}
	if i.DryRun {
		return mag.desiredRel, mag.dryRunUpgradeErr
	}
	return mag.desiredRel, mag.upgradeErr
}

func (mag *mockActionGetter) Uninstall(name string, opts ...helmclient.UninstallOption) (*release.UninstallReleaseResponse, error) {
	return nil, nil
}

func (mag *mockActionGetter) Reconcile(rel *release.Release) error {
	return mag.reconcileErr
}

var (
	// required for unmockable call to convert.RegistryV1ToHelmChart
	validFS = fstest.MapFS{
		"metadata/annotations.yaml": &fstest.MapFile{Data: []byte(`annotations:
  operators.operatorframework.io.bundle.package.v1: my-package`)},
		"manifests": &fstest.MapFile{Data: []byte(`apiVersion: operators.operatorframework.io/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: test.v1.0.0
  annotations:
    olm.properties: '[{"type":"from-csv-annotations-key", "value":"from-csv-annotations-value"}]'
spec:
  installModes:
    - type: SingleNamespace
      supported: true
    - type: OwnNamespace
      supported: true
    - type: AllNamespaces
      supported: true`)},
	}

	// required for unmockable call to util.ManifestObjects
	validManifest = `apiVersion: v1
kind: Service
metadata:
  name: service-a
  namespace: ns-a
spec:
  clusterIP: None
---
apiVersion: v1
kind: Service
metadata:
  name: service-b
  namespace: ns-b
spec:
  clusterIP: 0.0.0.0`

	testCE = &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "test-namespace",
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: "test-sa",
			},
		},
	}
	testObjectLabels  = map[string]string{"object": "label"}
	testStorageLabels = map[string]string{"storage": "label"}
	errPreAuth        = errors.New("problem running preauthorization")
	missingRBAC       = []authorization.ScopedPolicyRules{
		{
			Namespace: "",
			MissingRules: []rbacv1.PolicyRule{
				{
					Verbs:           []string{"list", "watch"},
					APIGroups:       []string{""},
					Resources:       []string{"services"},
					ResourceNames:   []string(nil),
					NonResourceURLs: []string(nil)},
			},
		},
		{
			Namespace: "test-namespace",
			MissingRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"*"},
					Resources: []string{"certificates"}},
			},
		},
	}

	errMissingRBAC = `pre-authorization failed: service account requires the following permissions to manage cluster extension:
  Namespace:"" APIGroups:[] Resources:[services] Verbs:[list,watch]
  Namespace:"test-namespace" APIGroups:[*] Resources:[certificates] Verbs:[create]`
)

func TestApply_Base(t *testing.T) {
	t.Run("fails converting content FS to helm chart", func(t *testing.T) {
		helmApplier := applier.Helm{}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), os.DirFS("/"), testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails trying to obtain an action client", func(t *testing.T) {
		mockAcg := &mockActionGetter{actionClientForErr: errors.New("failed getting action client")}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			HelmChartProvider:  DummyHelmChartProvider,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "getting action client")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails getting current release and !driver.ErrReleaseNotFound", func(t *testing.T) {
		mockAcg := &mockActionGetter{getClientErr: errors.New("failed getting current release")}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			HelmChartProvider:  DummyHelmChartProvider,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "getting current release")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})
}

func TestApply_Installation(t *testing.T) {
	t.Run("fails during dry-run installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr:     driver.ErrReleaseNotFound,
			dryRunInstallErr: errors.New("failed attempting to dry-run install chart"),
		}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			HelmChartProvider:  DummyHelmChartProvider,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "attempting to dry-run install chart")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during pre-flight installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			installErr:   errors.New("failed installing chart"),
		}
		mockPf := &mockPreflight{installErr: errors.New("failed during install pre-flight check")}
		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			Preflights:                    []applier.Preflight{mockPf},
			HelmChartProvider:             DummyHelmChartProvider,
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "install pre-flight check")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			installErr:   errors.New("failed installing chart"),
		}
		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			HelmChartProvider:             DummyHelmChartProvider,
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "installing chart")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("successful installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		}
		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			HelmChartProvider:             DummyHelmChartProvider,
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
			Manager: &mockManagedContentCacheManager{
				cache: &mockManagedContentCache{},
			},
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.NoError(t, err)
		require.Empty(t, installStatus)
		require.True(t, installSucceeded)
	})
}

func TestApply_InstallationWithPreflightPermissionsEnabled(t *testing.T) {
	t.Run("preauthorizer called with correct parameters", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			installErr:   errors.New("failed installing chart"),
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		}
		mockPf := &mockPreflight{installErr: errors.New("failed during install pre-flight check")}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			Preflights:         []applier.Preflight{mockPf},
			PreAuthorizer: &mockPreAuthorizer{
				fn: func(ctx context.Context, user user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
					t.Log("has correct user")
					require.Equal(t, "system:serviceaccount:test-namespace:test-sa", user.GetName())
					require.Empty(t, user.GetUID())
					require.Nil(t, user.GetExtra())
					require.Empty(t, user.GetGroups())

					t.Log("has correct additional permissions")
					require.Len(t, additionalRequiredPerms, 1)
					perms := additionalRequiredPerms[0](user)

					require.Len(t, perms, 1)
					require.Equal(t, authorizer.AttributesRecord{
						User:            user,
						Name:            "test-ext",
						APIGroup:        "olm.operatorframework.io",
						APIVersion:      "v1",
						Resource:        "clusterextensions/finalizers",
						ResourceRequest: true,
						Verb:            "update",
					}, perms[0])
					return nil, nil
				},
			},
			HelmChartProvider:             DummyHelmChartProvider,
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
		}

		_, _, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
	})

	t.Run("fails during dry-run installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr:     driver.ErrReleaseNotFound,
			dryRunInstallErr: errors.New("failed attempting to dry-run install chart"),
		}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			HelmChartProvider:  DummyHelmChartProvider,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "attempting to dry-run install chart")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during pre-flight installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			installErr:   errors.New("failed installing chart"),
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		}
		mockPf := &mockPreflight{installErr: errors.New("failed during install pre-flight check")}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			Preflights:         []applier.Preflight{mockPf},
			PreAuthorizer: &mockPreAuthorizer{
				fn: func(ctx context.Context, user user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
					return nil, nil
				},
			},
			HelmChartProvider:             DummyHelmChartProvider,
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "install pre-flight check")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during installation because of pre-authorization failure", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			PreAuthorizer: &mockPreAuthorizer{
				fn: func(ctx context.Context, user user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
					return nil, errPreAuth
				},
			},
			HelmChartProvider: DummyHelmChartProvider,
		}
		// Use a ClusterExtension with valid Spec fields.
		validCE := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "default",
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: "default",
				},
			},
		}
		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, validCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "problem running preauthorization")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during installation due to missing RBAC rules", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			PreAuthorizer: &mockPreAuthorizer{
				fn: func(ctx context.Context, user user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
					return missingRBAC, nil
				},
			},
			HelmChartProvider: DummyHelmChartProvider,
		}
		// Use a ClusterExtension with valid Spec fields.
		validCE := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "default",
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: "default",
				},
			},
		}
		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, validCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, errMissingRBAC)
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("successful installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			PreAuthorizer: &mockPreAuthorizer{
				fn: func(ctx context.Context, user user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
					return nil, nil
				},
			},
			HelmChartProvider:             DummyHelmChartProvider,
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
			Manager: &mockManagedContentCacheManager{
				cache: &mockManagedContentCache{},
			},
		}

		// Use a ClusterExtension with valid Spec fields.
		validCE := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "default",
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: "default",
				},
			},
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, validCE, testObjectLabels, testStorageLabels)
		require.NoError(t, err)
		require.Empty(t, installStatus)
		require.True(t, installSucceeded)
	})
}

func TestApply_Upgrade(t *testing.T) {
	testCurrentRelease := &release.Release{
		Info: &release.Info{Status: release.StatusDeployed},
	}

	t.Run("fails during dry-run upgrade", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			dryRunUpgradeErr: errors.New("failed attempting to dry-run upgrade chart"),
		}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			HelmChartProvider:  DummyHelmChartProvider,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "attempting to dry-run upgrade chart")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during pre-flight upgrade", func(t *testing.T) {
		testDesiredRelease := *testCurrentRelease
		testDesiredRelease.Manifest = "do-not-match-current"

		mockAcg := &mockActionGetter{
			upgradeErr: errors.New("failed upgrading chart"),
			currentRel: testCurrentRelease,
			desiredRel: &testDesiredRelease,
		}
		mockPf := &mockPreflight{upgradeErr: errors.New("failed during upgrade pre-flight check")}
		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			Preflights:                    []applier.Preflight{mockPf},
			HelmChartProvider:             DummyHelmChartProvider,
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "upgrade pre-flight check")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during upgrade", func(t *testing.T) {
		testDesiredRelease := *testCurrentRelease
		testDesiredRelease.Manifest = "do-not-match-current"

		mockAcg := &mockActionGetter{
			upgradeErr: errors.New("failed upgrading chart"),
			currentRel: testCurrentRelease,
			desiredRel: &testDesiredRelease,
		}
		mockPf := &mockPreflight{}
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg, Preflights: []applier.Preflight{mockPf},
			HelmChartProvider:             DummyHelmChartProvider,
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "upgrading chart")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during upgrade reconcile (StateUnchanged)", func(t *testing.T) {
		// make sure desired and current are the same this time
		testDesiredRelease := *testCurrentRelease

		mockAcg := &mockActionGetter{
			reconcileErr: errors.New("failed reconciling charts"),
			currentRel:   testCurrentRelease,
			desiredRel:   &testDesiredRelease,
		}
		mockPf := &mockPreflight{}
		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			Preflights:                    []applier.Preflight{mockPf},
			HelmChartProvider:             DummyHelmChartProvider,
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "reconciling charts")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("successful upgrade", func(t *testing.T) {
		testDesiredRelease := *testCurrentRelease
		testDesiredRelease.Manifest = validManifest

		mockAcg := &mockActionGetter{
			currentRel: testCurrentRelease,
			desiredRel: &testDesiredRelease,
		}
		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			HelmChartProvider:             DummyHelmChartProvider,
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
			Manager: &mockManagedContentCacheManager{
				cache: &mockManagedContentCache{},
			},
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.NoError(t, err)
		require.True(t, installSucceeded)
		require.Empty(t, installStatus)
	})
}

func TestApply_RegistryV1ToChartConverterIntegration(t *testing.T) {
	t.Run("generates bundle resources in AllNamespaces install mode", func(t *testing.T) {
		helmApplier := applier.Helm{
			ActionClientGetter: &mockActionGetter{
				getClientErr: driver.ErrReleaseNotFound,
				desiredRel: &release.Release{
					Info:     &release.Info{Status: release.StatusDeployed},
					Manifest: validManifest,
				},
			},
			HelmChartProvider: &FakeHelmChartProvider{
				fn: func(bundleFS fs.FS, ext *ocv1.ClusterExtension) (*chart.Chart, error) {
					require.Equal(t, testCE, ext)
					return nil, nil
				},
			},
			HelmReleaseToObjectsConverter: mockHelmReleaseToObjectsConverter{},
			Manager: &mockManagedContentCacheManager{
				cache: &mockManagedContentCache{},
			},
		}

		_, _, _ = helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
	})

	t.Run("surfaces chart generation errors", func(t *testing.T) {
		helmApplier := applier.Helm{
			ActionClientGetter: &mockActionGetter{
				getClientErr: driver.ErrReleaseNotFound,
				desiredRel: &release.Release{
					Info:     &release.Info{Status: release.StatusDeployed},
					Manifest: validManifest,
				},
			},
			HelmChartProvider: &FakeHelmChartProvider{
				fn: func(bundleFs fs.FS, ext *ocv1.ClusterExtension) (*chart.Chart, error) {
					return nil, errors.New("some error")
				},
			},
			Manager: &mockManagedContentCacheManager{
				cache: &mockManagedContentCache{},
			},
		}

		_, _, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.ErrorContains(t, err, "some error")
	})
}

type FakeHelmChartProvider struct {
	fn func(fs.FS, *ocv1.ClusterExtension) (*chart.Chart, error)
}

func (f FakeHelmChartProvider) Get(bundle fs.FS, ext *ocv1.ClusterExtension) (*chart.Chart, error) {
	return f.fn(bundle, ext)
}

var DummyHelmChartProvider = &FakeHelmChartProvider{
	fn: func(fs fs.FS, ext *ocv1.ClusterExtension) (*chart.Chart, error) {
		return &chart.Chart{}, nil
	},
}
