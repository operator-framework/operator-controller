package action

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mockhelmclient "github.com/operator-framework/operator-controller/internal/testutil/mock/helmclient"
)

func TestActionClientErrorTranslation(t *testing.T) {
	originalError := fmt.Errorf("some error")
	expectedErr := fmt.Errorf("something other error")
	errTranslator := func(originalErr error) error {
		return expectedErr
	}

	ctrl := gomock.NewController(t)
	ac := mockhelmclient.NewMockActionInterface(ctrl)

	ac.EXPECT().Get(gomock.Any(), gomock.Any()).Return(nil, originalError).AnyTimes()
	ac.EXPECT().History(gomock.Any(), gomock.Any()).Return(nil, originalError).AnyTimes()
	ac.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, originalError).AnyTimes()
	ac.EXPECT().Uninstall(gomock.Any(), gomock.Any()).Return(nil, originalError).AnyTimes()
	ac.EXPECT().Upgrade(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, originalError).AnyTimes()
	ac.EXPECT().Reconcile(gomock.Any()).Return(originalError).AnyTimes()

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
	ctrl := gomock.NewController(t)
	mockACG := mockhelmclient.NewMockActionClientGetter(ctrl)
	mockAI := mockhelmclient.NewMockActionInterface(ctrl)
	testError := errors.New("test error")

	first := mockACG.EXPECT().ActionClientFor(gomock.Any(), gomock.Any()).Return(mockAI, nil)
	mockACG.EXPECT().ActionClientFor(gomock.Any(), gomock.Any()).Return(nil, testError).After(first)

	acg := ActionClientGetter{
		ActionClientGetter: mockACG,
	}

	ctx := context.Background()
	var obj client.Object

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
