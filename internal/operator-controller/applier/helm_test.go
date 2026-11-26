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
	"go.uber.org/mock/gomock"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	mockhelmclient "github.com/operator-framework/operator-controller/internal/testutil/mock/helmclient"
)

type mockTrackingCache struct {
	watchErr error
	freeErr  error
}

func (m *mockTrackingCache) Watch(_ context.Context, _ client.Object, _ sets.Set[schema.GroupVersionKind]) error {
	return m.watchErr
}

func (m *mockTrackingCache) Free(_ context.Context, _ client.Object) error {
	return m.freeErr
}

type mockPreflight struct {
	installErr error
	upgradeErr error
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

func (mag *mockActionGetter) Config() *action.Configuration {
	return nil
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
		},
	}
	testObjectLabels  = map[string]string{"object": "label"}
	testStorageLabels = map[string]string{"storage": "label"}
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
			TrackingCache:                 &mockTrackingCache{},
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.NoError(t, err)
		require.Empty(t, installStatus)
		require.True(t, installSucceeded)
	})

	t.Run("succeeds when TrackingCache is nil (drift detection disabled)", func(t *testing.T) {
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
		}

		installSucceeded, installStatus, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
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
			TrackingCache:                 &mockTrackingCache{},
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
			TrackingCache:                 &mockTrackingCache{},
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
			TrackingCache: &mockTrackingCache{},
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

func TestHelmApply_MigratesLabelsWithoutCatalog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		manifest     string
		getErr       error
		reconcile    bool
		reconcileErr error
		wantErr      string
	}{
		{
			name:      "legacy labels are replaced",
			reconcile: true,
			manifest: `apiVersion: v1
kind: ConfigMap
metadata:
  name: managed
  labels:
    olm.operatorframework.io/owner-kind: ClusterExtension
    olm.operatorframework.io/owner-name: test-ext
    example.com/custom: keep
`,
		},
		{
			name:      "unlabelled resources are stamped",
			reconcile: true,
			manifest: `apiVersion: v1
kind: ConfigMap
metadata:
  name: managed
`,
		},
		{
			name:    "missing release is reported",
			getErr:  driver.ErrReleaseNotFound,
			wantErr: "catalog content unavailable and no release installed",
		},
		{
			name:    "read failure is reported",
			getErr:  errors.New("read failed"),
			wantErr: "failed to get current release: read failed",
		},
		{
			name:     "invalid stored manifest is reported",
			manifest: "metadata: [",
			wantErr:  "updating extension labels in release",
		},
		{
			name:         "reconcile failure is reported",
			manifest:     validManifest,
			reconcile:    true,
			reconcileErr: errors.New("write failed"),
			wantErr:      "write failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			ac := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
			stored := &release.Release{Name: testCE.Name, Manifest: tc.manifest}
			ac.EXPECT().ActionClientFor(gomock.Any(), testCE).Return(ac, nil)
			ac.EXPECT().Get(testCE.Name).Return(stored, tc.getErr)
			if tc.reconcile {
				ac.EXPECT().Reconcile(gomock.Any()).DoAndReturn(func(rel *release.Release) error {
					require.NotSame(t, stored, rel)
					objs, err := (applier.HelmReleaseToObjectsConverter{}).GetObjectsFromRelease(rel)
					require.NoError(t, err)
					require.NotEmpty(t, objs)
					for _, obj := range objs {
						require.Equal(t, testCE.Name, obj.GetLabels()[labels.ClusterExtensionNameKey])
						require.NotContains(t, obj.GetLabels(), "olm.operatorframework.io/owner-kind")
						require.NotContains(t, obj.GetLabels(), "olm.operatorframework.io/owner-name")
					}
					return tc.reconcileErr
				})
				if tc.reconcileErr == nil {
					ac.EXPECT().Config().Return(&action.Configuration{
						KubeClient: &kubefake.PrintingKubeClient{Out: io.Discard},
					})
				}
			}
			h := &applier.Helm{ActionClientGetter: ac, TrackingCache: &mockTrackingCache{}}
			complete, _, err := h.Apply(t.Context(), nil, testCE, nil, nil)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.False(t, complete)
			} else {
				require.NoError(t, err)
				require.True(t, complete)
			}
			require.Equal(t, tc.manifest, stored.Manifest, "stored release history must remain untouched")
		})
	}
}
