package applier_test

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authorization"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
	mockauthorization "github.com/operator-framework/operator-controller/internal/testutil/mock/authorization"
	mockcmcache "github.com/operator-framework/operator-controller/internal/testutil/mock/cmcache"
	mockcontentmanager "github.com/operator-framework/operator-controller/internal/testutil/mock/contentmanager"
	mockhelmclient "github.com/operator-framework/operator-controller/internal/testutil/mock/helmclient"
)

type mockActionGetterConfig struct {
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

func newMockActionGetter(ctrl *gomock.Controller, cfg mockActionGetterConfig) *mockhelmclient.MockActionClientGetterAndInterface {
	m := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)

	if cfg.actionClientForErr != nil {
		m.EXPECT().ActionClientFor(gomock.Any(), gomock.Any()).Return(nil, cfg.actionClientForErr).AnyTimes()
	} else {
		m.EXPECT().ActionClientFor(gomock.Any(), gomock.Any()).Return(m, nil).AnyTimes()
	}

	m.EXPECT().Get(gomock.Any(), gomock.Any()).Return(cfg.currentRel, cfg.getClientErr).AnyTimes()
	m.EXPECT().History(gomock.Any(), gomock.Any()).Return(cfg.history, cfg.historyErr).AnyTimes()
	m.EXPECT().Config().Return(nil).AnyTimes()
	m.EXPECT().Reconcile(gomock.Any()).Return(cfg.reconcileErr).AnyTimes()
	m.EXPECT().Uninstall(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	// Install with dry-run support
	m.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(name, ns string, chrt *chart.Chart, vals map[string]interface{}, opts ...helmclient.InstallOption) (*release.Release, error) {
			i := action.Install{}
			for _, opt := range opts {
				if err := opt(&i); err != nil {
					return nil, err
				}
			}
			if i.DryRun {
				return cfg.desiredRel, cfg.dryRunInstallErr
			}
			return cfg.desiredRel, cfg.installErr
		}).AnyTimes()

	// Upgrade with dry-run support
	m.EXPECT().Upgrade(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(name, ns string, chrt *chart.Chart, vals map[string]interface{}, opts ...helmclient.UpgradeOption) (*release.Release, error) {
			u := action.Upgrade{}
			for _, opt := range opts {
				if err := opt(&u); err != nil {
					return nil, err
				}
			}
			if u.DryRun {
				return cfg.desiredRel, cfg.dryRunUpgradeErr
			}
			return cfg.desiredRel, cfg.upgradeErr
		}).AnyTimes()

	return m
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
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{actionClientForErr: errors.New("failed getting action client")})
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			HelmChartProvider:  newDummyHelmChartProvider(ctrl),
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "getting action client")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails getting current release and !driver.ErrReleaseNotFound", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{getClientErr: errors.New("failed getting current release")})
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			HelmChartProvider:  newDummyHelmChartProvider(ctrl),
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
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr:     driver.ErrReleaseNotFound,
			dryRunInstallErr: errors.New("failed attempting to dry-run install chart"),
		})
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			HelmChartProvider:  newDummyHelmChartProvider(ctrl),
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "attempting to dry-run install chart")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during pre-flight installation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr: driver.ErrReleaseNotFound,
			installErr:   errors.New("failed installing chart"),
		})
		mockPf := mockapplier.NewMockPreflight(ctrl)
		mockPf.EXPECT().Install(gomock.Any(), gomock.Any()).Return(errors.New("failed during install pre-flight check")).AnyTimes()
		mockPf.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			Preflights:                    []applier.Preflight{mockPf},
			HelmChartProvider:             newDummyHelmChartProvider(ctrl),
			HelmReleaseToObjectsConverter: mockConverter,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "install pre-flight check")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during installation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr: driver.ErrReleaseNotFound,
			installErr:   errors.New("failed installing chart"),
		})
		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			HelmChartProvider:             newDummyHelmChartProvider(ctrl),
			HelmReleaseToObjectsConverter: mockConverter,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "installing chart")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("successful installation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr: driver.ErrReleaseNotFound,
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		})
		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		mockCache := mockcmcache.NewMockCache(ctrl)
		mockCache.EXPECT().Close().Return(nil).AnyTimes()
		mockCache.EXPECT().Watch(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		mockMgr := mockcontentmanager.NewMockManager(ctrl)
		mockMgr.EXPECT().Get(gomock.Any(), gomock.Any()).Return(mockCache, nil).AnyTimes()
		mockMgr.EXPECT().Delete(gomock.Any()).Return(nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			HelmChartProvider:             newDummyHelmChartProvider(ctrl),
			HelmReleaseToObjectsConverter: mockConverter,
			Manager:                       mockMgr,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.NoError(t, err)
		require.Empty(t, installStatus)
		require.True(t, installSucceeded)
	})
}

func TestApply_InstallationWithPreflightPermissionsEnabled(t *testing.T) {
	t.Run("preauthorizer called with correct parameters", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr: driver.ErrReleaseNotFound,
			installErr:   errors.New("failed installing chart"),
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		})
		mockPf := mockapplier.NewMockPreflight(ctrl)
		mockPf.EXPECT().Install(gomock.Any(), gomock.Any()).Return(errors.New("failed during install pre-flight check")).AnyTimes()
		mockPf.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		mockPA := mockauthorization.NewMockPreAuthorizer(ctrl)
		mockPA.EXPECT().PreAuthorize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, userInfo user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
				t.Log("has correct user")
				require.Equal(t, "system:serviceaccount:test-namespace:test-sa", userInfo.GetName())
				require.Empty(t, userInfo.GetUID())
				require.Nil(t, userInfo.GetExtra())
				require.Empty(t, userInfo.GetGroups())

				t.Log("has correct additional permissions")
				require.Len(t, additionalRequiredPerms, 1)
				perms := additionalRequiredPerms[0](userInfo)

				require.Len(t, perms, 1)
				require.Equal(t, authorizer.AttributesRecord{
					User:            userInfo,
					Name:            "test-ext",
					APIGroup:        "olm.operatorframework.io",
					APIVersion:      "v1",
					Resource:        "clusterextensions/finalizers",
					ResourceRequest: true,
					Verb:            "update",
				}, perms[0])
				return nil, nil
			}).AnyTimes()

		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			Preflights:                    []applier.Preflight{mockPf},
			PreAuthorizer:                 mockPA,
			HelmChartProvider:             newDummyHelmChartProvider(ctrl),
			HelmReleaseToObjectsConverter: mockConverter,
		}

		_, _, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
	})

	t.Run("fails during dry-run installation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr:     driver.ErrReleaseNotFound,
			dryRunInstallErr: errors.New("failed attempting to dry-run install chart"),
		})
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			HelmChartProvider:  newDummyHelmChartProvider(ctrl),
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "attempting to dry-run install chart")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during pre-flight installation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr: driver.ErrReleaseNotFound,
			installErr:   errors.New("failed installing chart"),
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		})
		mockPf := mockapplier.NewMockPreflight(ctrl)
		mockPf.EXPECT().Install(gomock.Any(), gomock.Any()).Return(errors.New("failed during install pre-flight check")).AnyTimes()
		mockPf.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		mockPA := mockauthorization.NewMockPreAuthorizer(ctrl)
		mockPA.EXPECT().PreAuthorize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			Preflights:                    []applier.Preflight{mockPf},
			PreAuthorizer:                 mockPA,
			HelmChartProvider:             newDummyHelmChartProvider(ctrl),
			HelmReleaseToObjectsConverter: mockConverter,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "install pre-flight check")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during installation because of pre-authorization failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr: driver.ErrReleaseNotFound,
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		})
		mockPA := mockauthorization.NewMockPreAuthorizer(ctrl)
		mockPA.EXPECT().PreAuthorize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errPreAuth).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			PreAuthorizer:      mockPA,
			HelmChartProvider:  newDummyHelmChartProvider(ctrl),
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
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr: driver.ErrReleaseNotFound,
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		})
		mockPA := mockauthorization.NewMockPreAuthorizer(ctrl)
		mockPA.EXPECT().PreAuthorize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(missingRBAC, nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			PreAuthorizer:      mockPA,
			HelmChartProvider:  newDummyHelmChartProvider(ctrl),
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
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr: driver.ErrReleaseNotFound,
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		})
		mockPA := mockauthorization.NewMockPreAuthorizer(ctrl)
		mockPA.EXPECT().PreAuthorize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		mockCache := mockcmcache.NewMockCache(ctrl)
		mockCache.EXPECT().Close().Return(nil).AnyTimes()
		mockCache.EXPECT().Watch(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		mockMgr := mockcontentmanager.NewMockManager(ctrl)
		mockMgr.EXPECT().Get(gomock.Any(), gomock.Any()).Return(mockCache, nil).AnyTimes()
		mockMgr.EXPECT().Delete(gomock.Any()).Return(nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			PreAuthorizer:                 mockPA,
			HelmChartProvider:             newDummyHelmChartProvider(ctrl),
			HelmReleaseToObjectsConverter: mockConverter,
			Manager:                       mockMgr,
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
		ctrl := gomock.NewController(t)
		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			dryRunUpgradeErr: errors.New("failed attempting to dry-run upgrade chart"),
		})
		helmApplier := applier.Helm{
			ActionClientGetter: mockAcg,
			HelmChartProvider:  newDummyHelmChartProvider(ctrl),
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "attempting to dry-run upgrade chart")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during pre-flight upgrade", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		testDesiredRelease := *testCurrentRelease
		testDesiredRelease.Manifest = "do-not-match-current"

		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			upgradeErr: errors.New("failed upgrading chart"),
			currentRel: testCurrentRelease,
			desiredRel: &testDesiredRelease,
		})
		mockPf := mockapplier.NewMockPreflight(ctrl)
		mockPf.EXPECT().Install(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockPf.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(errors.New("failed during upgrade pre-flight check")).AnyTimes()

		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			Preflights:                    []applier.Preflight{mockPf},
			HelmChartProvider:             newDummyHelmChartProvider(ctrl),
			HelmReleaseToObjectsConverter: mockConverter,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "upgrade pre-flight check")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during upgrade", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		testDesiredRelease := *testCurrentRelease
		testDesiredRelease.Manifest = "do-not-match-current"

		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			upgradeErr: errors.New("failed upgrading chart"),
			currentRel: testCurrentRelease,
			desiredRel: &testDesiredRelease,
		})
		mockPf := mockapplier.NewMockPreflight(ctrl)
		mockPf.EXPECT().Install(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockPf.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			Preflights:                    []applier.Preflight{mockPf},
			HelmChartProvider:             newDummyHelmChartProvider(ctrl),
			HelmReleaseToObjectsConverter: mockConverter,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "upgrading chart")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("fails during upgrade reconcile (StateUnchanged)", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		// make sure desired and current are the same this time
		testDesiredRelease := *testCurrentRelease

		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			reconcileErr: errors.New("failed reconciling charts"),
			currentRel:   testCurrentRelease,
			desiredRel:   &testDesiredRelease,
		})
		mockPf := mockapplier.NewMockPreflight(ctrl)
		mockPf.EXPECT().Install(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockPf.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			Preflights:                    []applier.Preflight{mockPf},
			HelmChartProvider:             newDummyHelmChartProvider(ctrl),
			HelmReleaseToObjectsConverter: mockConverter,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "reconciling charts")
		require.False(t, installSucceeded)
		require.Empty(t, installStatus)
	})

	t.Run("successful upgrade", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		testDesiredRelease := *testCurrentRelease
		testDesiredRelease.Manifest = validManifest

		mockAcg := newMockActionGetter(ctrl, mockActionGetterConfig{
			currentRel: testCurrentRelease,
			desiredRel: &testDesiredRelease,
		})
		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		mockCache := mockcmcache.NewMockCache(ctrl)
		mockCache.EXPECT().Close().Return(nil).AnyTimes()
		mockCache.EXPECT().Watch(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		mockMgr := mockcontentmanager.NewMockManager(ctrl)
		mockMgr.EXPECT().Get(gomock.Any(), gomock.Any()).Return(mockCache, nil).AnyTimes()
		mockMgr.EXPECT().Delete(gomock.Any()).Return(nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter:            mockAcg,
			HelmChartProvider:             newDummyHelmChartProvider(ctrl),
			HelmReleaseToObjectsConverter: mockConverter,
			Manager:                       mockMgr,
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.NoError(t, err)
		require.True(t, installSucceeded)
		require.Empty(t, installStatus)
	})
}

func TestApply_RegistryV1ToChartConverterIntegration(t *testing.T) {
	t.Run("generates bundle resources in AllNamespaces install mode", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockChartProvider := mockapplier.NewMockHelmChartProvider(ctrl)
		mockChartProvider.EXPECT().Get(gomock.Any(), testCE).Return(nil, nil).Times(1)

		mockConverter := mockapplier.NewMockHelmReleaseToObjectsConverterInterface(ctrl)
		mockConverter.EXPECT().GetObjectsFromRelease(gomock.Any()).Return(nil, nil).AnyTimes()

		mockCache := mockcmcache.NewMockCache(ctrl)
		mockCache.EXPECT().Close().Return(nil).AnyTimes()
		mockCache.EXPECT().Watch(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		mockMgr := mockcontentmanager.NewMockManager(ctrl)
		mockMgr.EXPECT().Get(gomock.Any(), gomock.Any()).Return(mockCache, nil).AnyTimes()
		mockMgr.EXPECT().Delete(gomock.Any()).Return(nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter: newMockActionGetter(ctrl, mockActionGetterConfig{
				getClientErr: driver.ErrReleaseNotFound,
				desiredRel: &release.Release{
					Info:     &release.Info{Status: release.StatusDeployed},
					Manifest: validManifest,
				},
			}),
			HelmChartProvider:             mockChartProvider,
			HelmReleaseToObjectsConverter: mockConverter,
			Manager:                       mockMgr,
		}

		_, _, _ = helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
	})

	t.Run("surfaces chart generation errors", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockChartProvider := mockapplier.NewMockHelmChartProvider(ctrl)
		mockChartProvider.EXPECT().Get(gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).AnyTimes()

		mockCache := mockcmcache.NewMockCache(ctrl)
		mockCache.EXPECT().Close().Return(nil).AnyTimes()
		mockCache.EXPECT().Watch(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		mockMgr := mockcontentmanager.NewMockManager(ctrl)
		mockMgr.EXPECT().Get(gomock.Any(), gomock.Any()).Return(mockCache, nil).AnyTimes()
		mockMgr.EXPECT().Delete(gomock.Any()).Return(nil).AnyTimes()

		helmApplier := applier.Helm{
			ActionClientGetter: newMockActionGetter(ctrl, mockActionGetterConfig{
				getClientErr: driver.ErrReleaseNotFound,
				desiredRel: &release.Release{
					Info:     &release.Info{Status: release.StatusDeployed},
					Manifest: validManifest,
				},
			}),
			HelmChartProvider: mockChartProvider,
			Manager:           mockMgr,
		}

		_, _, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.ErrorContains(t, err, "some error")
	})
}

func newDummyHelmChartProvider(ctrl *gomock.Controller) *mockapplier.MockHelmChartProvider {
	m := mockapplier.NewMockHelmChartProvider(ctrl)
	m.EXPECT().Get(gomock.Any(), gomock.Any()).Return(&chart.Chart{}, nil).AnyTimes()
	return m
}
