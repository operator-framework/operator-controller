package action

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"sigs.k8s.io/controller-runtime/pkg/client"

	actionclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
)

var _ actionclient.ActionInterface = &mockActionClient{}

type mockActionClient struct {
	mock.Mock
}

func (m *mockActionClient) Get(name string, opts ...actionclient.GetOption) (*release.Release, error) {
	args := m.Called(name, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*release.Release), args.Error(1)
}

func (m *mockActionClient) History(name string, opts ...actionclient.HistoryOption) ([]*release.Release, error) {
	args := m.Called(name, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	rel := []*release.Release{
		args.Get(0).(*release.Release),
	}
	return rel, args.Error(1)
}

func (m *mockActionClient) Install(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...actionclient.InstallOption) (*release.Release, error) {
	args := m.Called(name, namespace, chrt, vals, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*release.Release), args.Error(1)
}

func (m *mockActionClient) Upgrade(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...actionclient.UpgradeOption) (*release.Release, error) {
	args := m.Called(name, namespace, chrt, vals, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*release.Release), args.Error(1)
}

func (m *mockActionClient) Uninstall(name string, opts ...actionclient.UninstallOption) (*release.UninstallReleaseResponse, error) {
	args := m.Called(name, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*release.UninstallReleaseResponse), args.Error(1)
}

func (m *mockActionClient) Reconcile(rel *release.Release) error {
	args := m.Called(rel)
	return args.Error(0)
}

func (m *mockActionClient) Config() *action.Configuration {
	args := m.Called()
	return args.Get(0).(*action.Configuration)
}

var _ actionclient.ActionClientGetter = &mockActionClientGetter{}

type mockActionClientGetter struct {
	mock.Mock
}

func (m *mockActionClientGetter) ActionClientFor(ctx context.Context, obj client.Object) (actionclient.ActionInterface, error) {
	args := m.Called(ctx, obj)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(actionclient.ActionInterface), args.Error(1)
}

func TestActionClientErrorTranslation(t *testing.T) {
	originalError := fmt.Errorf("some error")
	expectedErr := fmt.Errorf("something other error")
	errTranslator := func(originalErr error) error {
		return expectedErr
	}

	ac := new(mockActionClient)
	ac.On("Get", mock.Anything, mock.Anything).Return(nil, originalError)
	ac.On("History", mock.Anything, mock.Anything).Return(nil, originalError)
	ac.On("Install", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, originalError)
	ac.On("Uninstall", mock.Anything, mock.Anything).Return(nil, originalError)
	ac.On("Upgrade", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, originalError)
	ac.On("Reconcile", mock.Anything, mock.Anything).Return(originalError)

	wrappedAc := NewWrappedActionClient(ac, errTranslator)

	// Get
	_, err := wrappedAc.Get("something")
	assert.Equal(t, expectedErr, err, "expected Get() to return translated error")

	// History
	_, err = wrappedAc.History("something")
	assert.Equal(t, expectedErr, err, "expected History() to return translated error")

	// Install
	_, err = wrappedAc.Install("something", "somethingelse", nil, nil)
	assert.Equal(t, expectedErr, err, "expected Install() to return translated error")

	// Uninstall
	_, err = wrappedAc.Uninstall("something")
	assert.Equal(t, expectedErr, err, "expected Uninstall() to return translated error")

	// Upgrade
	_, err = wrappedAc.Upgrade("something", "somethingelse", nil, nil)
	assert.Equal(t, expectedErr, err, "expected Upgrade() to return translated error")

	// Reconcile
	err = wrappedAc.Reconcile(nil)
	assert.Equal(t, expectedErr, err, "expected Reconcile() to return translated error")
}

func TestActionClientFor(t *testing.T) {
	// Create a mock for the ActionClientGetter
	mockActionClientGetter := new(mockActionClientGetter)
	mockActionInterface := new(mockActionClient)
	testError := errors.New("test error")

	// Set up expectations for the mock
	mockActionClientGetter.On("ActionClientFor", mock.Anything, mock.Anything).Return(mockActionInterface, nil).Once()
	mockActionClientGetter.On("ActionClientFor", mock.Anything, mock.Anything).Return(nil, testError).Once()

	// Create an instance of ActionClientGetter with the mock
	acg := ActionClientGetter{
		ActionClientGetter: mockActionClientGetter,
	}

	// Define a test context and object
	ctx := context.Background()
	var obj client.Object // Replace with an actual client.Object implementation

	// Test the successful case
	actionClient, err := acg.ActionClientFor(ctx, obj)
	require.NoError(t, err)
	assert.NotNil(t, actionClient)
	assert.IsType(t, &ActionClient{}, actionClient)

	// Test the error case
	actionClient, err = acg.ActionClientFor(ctx, obj)
	require.Error(t, err)
	assert.Nil(t, actionClient)
}
