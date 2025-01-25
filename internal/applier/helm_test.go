package applier_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	"sigs.k8s.io/controller-runtime/pkg/client"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	v1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/applier"
)

type mockPreflight struct {
	installErr error
	upgradeErr error
}

func (mp *mockPreflight) Install(context.Context, *release.Release) error {
	return mp.installErr
}

func (mp *mockPreflight) Upgrade(context.Context, *release.Release) error {
	return mp.upgradeErr
}

type mockActionGetter struct {
	actionClientForErr error
	getClientErr       error
	installErr         error
	dryRunInstallErr   error
	upgradeErr         error
	dryRunUpgradeErr   error
	reconcileErr       error
	desiredRel         *release.Release
	currentRel         *release.Release
}

func (mag *mockActionGetter) ActionClientFor(ctx context.Context, obj client.Object) (helmclient.ActionInterface, error) {
	return mag, mag.actionClientForErr
}

func (mag *mockActionGetter) Get(name string, opts ...helmclient.GetOption) (*release.Release, error) {
	return mag.currentRel, mag.getClientErr
}

func (mag *mockActionGetter) History(name string, opts ...helmclient.HistoryOption) ([]*release.Release, error) {
	return nil, mag.getClientErr
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
		"metadata/annotations.yaml": &fstest.MapFile{Data: []byte("{}")},
		"manifests": &fstest.MapFile{Data: []byte(`apiVersion: operators.operatorframework.io/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: test.v1.0.0
  annotations:
    olm.properties: '[{"type":"from-csv-annotations-key", "value":"from-csv-annotations-value"}]'
spec:
  installModes:
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

	testCE            = &v1.ClusterExtension{}
	testObjectLabels  = map[string]string{"object": "label"}
	testStorageLabels = map[string]string{"storage": "label"}
)

func TestApply_Base(t *testing.T) {
	t.Run("fails converting content FS to helm chart", func(t *testing.T) {
		helmApplier := applier.Helm{}

		objs, state, err := helmApplier.Apply(context.TODO(), os.DirFS("/"), testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.Nil(t, objs)
		require.Empty(t, state)
	})

	t.Run("fails trying to obtain an action client", func(t *testing.T) {
		mockAcg := &mockActionGetter{actionClientForErr: errors.New("failed getting action client")}
		helmApplier := applier.Helm{ActionClientGetter: mockAcg}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "getting action client")
		require.Nil(t, objs)
		require.Empty(t, state)
	})

	t.Run("fails getting current release and !driver.ErrReleaseNotFound", func(t *testing.T) {
		mockAcg := &mockActionGetter{getClientErr: errors.New("failed getting current release")}
		helmApplier := applier.Helm{ActionClientGetter: mockAcg}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "getting current release")
		require.Nil(t, objs)
		require.Empty(t, state)
	})
}

func TestApply_Installation(t *testing.T) {
	t.Run("fails during dry-run installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr:     driver.ErrReleaseNotFound,
			dryRunInstallErr: errors.New("failed attempting to dry-run install chart"),
		}
		helmApplier := applier.Helm{ActionClientGetter: mockAcg}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "attempting to dry-run install chart")
		require.Nil(t, objs)
		require.Empty(t, state)
	})

	t.Run("fails during pre-flight installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			installErr:   errors.New("failed installing chart"),
		}
		mockPf := &mockPreflight{installErr: errors.New("failed during install pre-flight check")}
		helmApplier := applier.Helm{ActionClientGetter: mockAcg, Preflights: []applier.Preflight{mockPf}}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "install pre-flight check")
		require.Equal(t, applier.StateNeedsInstall, state)
		require.Nil(t, objs)
	})

	t.Run("fails during installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			installErr:   errors.New("failed installing chart"),
		}
		helmApplier := applier.Helm{ActionClientGetter: mockAcg}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "installing chart")
		require.Equal(t, applier.StateNeedsInstall, state)
		require.Nil(t, objs)
	})

	t.Run("successful installation", func(t *testing.T) {
		mockAcg := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
			desiredRel: &release.Release{
				Info:     &release.Info{Status: release.StatusDeployed},
				Manifest: validManifest,
			},
		}
		helmApplier := applier.Helm{ActionClientGetter: mockAcg}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.NoError(t, err)
		require.Equal(t, applier.StateNeedsInstall, state)
		require.NotNil(t, objs)
		assert.Equal(t, "service-a", objs[0].GetName())
		assert.Equal(t, "service-b", objs[1].GetName())
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
		helmApplier := applier.Helm{ActionClientGetter: mockAcg}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "attempting to dry-run upgrade chart")
		require.Nil(t, objs)
		require.Empty(t, state)
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
		helmApplier := applier.Helm{ActionClientGetter: mockAcg, Preflights: []applier.Preflight{mockPf}}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "upgrade pre-flight check")
		require.Equal(t, applier.StateNeedsUpgrade, state)
		require.Nil(t, objs)
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
		helmApplier := applier.Helm{ActionClientGetter: mockAcg, Preflights: []applier.Preflight{mockPf}}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "upgrading chart")
		require.Equal(t, applier.StateNeedsUpgrade, state)
		require.Nil(t, objs)
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
		helmApplier := applier.Helm{ActionClientGetter: mockAcg, Preflights: []applier.Preflight{mockPf}}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.Error(t, err)
		require.ErrorContains(t, err, "reconciling charts")
		require.Equal(t, applier.StateUnchanged, state)
		require.Nil(t, objs)
	})

	t.Run("successful upgrade", func(t *testing.T) {
		testDesiredRelease := *testCurrentRelease
		testDesiredRelease.Manifest = validManifest

		mockAcg := &mockActionGetter{
			currentRel: testCurrentRelease,
			desiredRel: &testDesiredRelease,
		}
		helmApplier := applier.Helm{ActionClientGetter: mockAcg}

		objs, state, err := helmApplier.Apply(context.TODO(), validFS, testCE, testObjectLabels, testStorageLabels)
		require.NoError(t, err)
		require.Equal(t, applier.StateNeedsUpgrade, state)
		require.NotNil(t, objs)
		assert.Equal(t, "service-a", objs[0].GetName())
		assert.Equal(t, "service-b", objs[1].GetName())
	})
}
