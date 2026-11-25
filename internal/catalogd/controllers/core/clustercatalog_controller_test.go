package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/docker/reference"
	"go.uber.org/mock/gomock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
	mockstorage "github.com/operator-framework/operator-controller/internal/testutil/mock/storage"
)

func newMockStore(ctrl *gomock.Controller, shouldError bool) *mockstorage.MockInstance {
	m := mockstorage.NewMockInstance(ctrl)
	if shouldError {
		m.EXPECT().Store(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("mockstore store error")).AnyTimes()
		m.EXPECT().Delete(gomock.Any()).Return(errors.New("mockstore delete error")).AnyTimes()
	} else {
		m.EXPECT().Store(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		m.EXPECT().Delete(gomock.Any()).Return(nil).AnyTimes()
	}
	m.EXPECT().BaseURL(gomock.Any()).Return("URL").AnyTimes()
	m.EXPECT().ContentExists(gomock.Any()).Return(true).AnyTimes()
	return m
}

func TestCatalogdControllerReconcile(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	for _, tt := range []struct {
		name            string
		catalog         *ocv1.ClusterCatalog
		expectedError   error
		expectedCatalog *ocv1.ClusterCatalog
		puller          imageutil.Puller
		cache           imageutil.Cache
		store           storage.Instance
	}{
		{
			name:   "invalid source type, returns error",
			puller: &imageutil.FakePuller{},
			store:  newMockStore(mockCtrl, false),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: "invalid",
					},
				},
			},
			expectedError: reconcile.TerminalError(errors.New(`unknown source type "invalid"`)),
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: "invalid",
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonBlocked,
						},
					},
				},
			},
		},
		{
			name:          "valid source type, unpack returns error, status updated to reflect error state and error is returned",
			expectedError: fmt.Errorf("source catalog content: %w", fmt.Errorf("mockpuller error")),
			puller: &imageutil.FakePuller{
				Error: errors.New("mockpuller error"),
			},
			store: newMockStore(mockCtrl, false),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonRetrying,
						},
					},
				},
			},
		},
		{
			name:          "valid source type, unpack returns terminal error, status updated to reflect terminal error state(Blocked) and error is returned",
			expectedError: fmt.Errorf("source catalog content: %w", reconcile.TerminalError(fmt.Errorf("mockpuller terminal error"))),
			puller: &imageutil.FakePuller{
				Error: reconcile.TerminalError(errors.New("mockpuller terminal error")),
			},
			store: newMockStore(mockCtrl, false),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonBlocked,
						},
					},
				},
			},
		},
		{
			name: "valid source type, unpack state == Unpacked, should reflect in status that it's progressing, and is serving",
			puller: &imageutil.FakePuller{
				ImageFS: &fstest.MapFS{},
				Ref:     mustRef(t, "my.org/someimage@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"),
			},
			store: newMockStore(mockCtrl, false),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					URLs: &ocv1.ClusterCatalogURLs{Base: "URL"},
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonAvailable,
						},
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonSucceeded,
						},
					},
					ResolvedSource: &ocv1.ResolvedCatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ResolvedImageSource{
							Ref: "my.org/someimage@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
						},
					},
					LastUnpacked: &metav1.Time{},
				},
			},
		},
		{
			name:          "valid source type, unpack state == Unpacked, storage fails, failure reflected in status and error returned",
			expectedError: fmt.Errorf("error storing fbc: mockstore store error"),
			puller: &imageutil.FakePuller{
				ImageFS: &fstest.MapFS{},
			},
			store: newMockStore(mockCtrl, true),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonRetrying,
						},
					},
				},
			},
		},
		{
			name: "storage finalizer not set, storage finalizer gets set",
			puller: &imageutil.FakePuller{
				ImageFS: &fstest.MapFS{},
			},
			store: newMockStore(mockCtrl, false),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "catalog",
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
			},
		},
		{
			name: "storage finalizer set, catalog deletion timestamp is not zero (or nil), finalizer removed",
			puller: &imageutil.FakePuller{
				ImageFS: &fstest.MapFS{},
			},
			store: newMockStore(mockCtrl, false),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "catalog",
					Finalizers:        []string{fbcDeletionFinalizer},
					DeletionTimestamp: &metav1.Time{Time: time.Date(2023, time.October, 10, 4, 19, 0, 0, time.UTC)},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					LastUnpacked: &metav1.Time{},
					ResolvedSource: &ocv1.ResolvedCatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ResolvedImageSource{
							Ref: "",
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonAvailable,
						},
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonSucceeded,
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "catalog",
					Finalizers:        []string{},
					DeletionTimestamp: &metav1.Time{Time: time.Date(2023, time.October, 10, 4, 19, 0, 0, time.UTC)},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonUnavailable,
						},
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonSucceeded,
						},
					},
				},
			},
		},
		{
			name:          "storage finalizer set, catalog deletion timestamp is not zero (or nil), storage delete failed, error returned, finalizer not removed and catalog continues serving",
			expectedError: fmt.Errorf("finalizer %q failed: %w", fbcDeletionFinalizer, fmt.Errorf("mockstore delete error")),
			puller: &imageutil.FakePuller{
				ImageFS: &fstest.MapFS{},
			},
			store: newMockStore(mockCtrl, true),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "catalog",
					Finalizers:        []string{fbcDeletionFinalizer},
					DeletionTimestamp: &metav1.Time{Time: time.Date(2023, time.October, 10, 4, 19, 0, 0, time.UTC)},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					URLs: &ocv1.ClusterCatalogURLs{Base: "URL"},
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonSucceeded,
						},
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonAvailable,
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "catalog",
					Finalizers:        []string{fbcDeletionFinalizer},
					DeletionTimestamp: &metav1.Time{Time: time.Date(2023, time.October, 10, 4, 19, 0, 0, time.UTC)},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					URLs: &ocv1.ClusterCatalogURLs{Base: "URL"},
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonRetrying,
						},
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonAvailable,
						},
					},
				},
			},
		},
		{
			name:          "storage finalizer set, catalog deletion timestamp is not zero (or nil), unpack cleanup failed, error returned, finalizer not removed but catalog stops serving",
			expectedError: fmt.Errorf("finalizer %q failed: %w", fbcDeletionFinalizer, fmt.Errorf("mockcache delete error")),
			puller:        &imageutil.FakePuller{},
			cache:         &imageutil.FakeCache{DeleteErr: fmt.Errorf("mockcache delete error")},
			store:         newMockStore(mockCtrl, false),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "catalog",
					Finalizers:        []string{fbcDeletionFinalizer},
					DeletionTimestamp: &metav1.Time{Time: time.Date(2023, time.October, 10, 4, 19, 0, 0, time.UTC)},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonSucceeded,
						},
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonAvailable,
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "catalog",
					Finalizers:        []string{fbcDeletionFinalizer},
					DeletionTimestamp: &metav1.Time{Time: time.Date(2023, time.October, 10, 4, 19, 0, 0, time.UTC)},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: ocv1.ClusterCatalogStatus{
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonRetrying,
						},
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonUnavailable,
						},
					},
				},
			},
		},
		{
			name: "catalog availability set to disabled, status.urls should get unset",
			puller: &imageutil.FakePuller{
				ImageFS: &fstest.MapFS{},
			},
			store: newMockStore(mockCtrl, false),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "catalog",
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
					AvailabilityMode: ocv1.AvailabilityModeUnavailable,
				},
				Status: ocv1.ClusterCatalogStatus{
					URLs:         &ocv1.ClusterCatalogURLs{Base: "URL"},
					LastUnpacked: &metav1.Time{},
					ResolvedSource: &ocv1.ResolvedCatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ResolvedImageSource{
							Ref: "",
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonAvailable,
						},
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonSucceeded,
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "catalog",
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
					AvailabilityMode: ocv1.AvailabilityModeUnavailable,
				},
				Status: ocv1.ClusterCatalogStatus{
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonUserSpecifiedUnavailable,
						},
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonSucceeded,
						},
					},
				},
			},
		},
		{
			name: "catalog availability set to disabled, finalizer should get removed",
			puller: &imageutil.FakePuller{
				ImageFS: &fstest.MapFS{},
			},
			store: newMockStore(mockCtrl, false),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
					AvailabilityMode: ocv1.AvailabilityModeUnavailable,
				},
				Status: ocv1.ClusterCatalogStatus{
					URLs:         &ocv1.ClusterCatalogURLs{Base: "URL"},
					LastUnpacked: &metav1.Time{},
					ResolvedSource: &ocv1.ResolvedCatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ResolvedImageSource{
							Ref: "",
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonAvailable,
						},
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonSucceeded,
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
					AvailabilityMode: ocv1.AvailabilityModeUnavailable,
				},
				Status: ocv1.ClusterCatalogStatus{
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonUserSpecifiedUnavailable,
						},
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ClusterCatalogReasonSucceeded,
						},
					},
				},
			},
		},
		{
			name: "after catalog availability set to enable, finalizer should be added",
			puller: &imageutil.FakePuller{
				ImageFS: &fstest.MapFS{},
			},
			store: newMockStore(mockCtrl, false),
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
					AvailabilityMode: ocv1.AvailabilityModeAvailable,
				},
				Status: ocv1.ClusterCatalogStatus{
					URLs: &ocv1.ClusterCatalogURLs{Base: "URL"},
					ResolvedSource: &ocv1.ResolvedCatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ResolvedImageSource{
							Ref: "",
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonUnavailable,
						},
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonUserSpecifiedUnavailable,
						},
					},
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
					AvailabilityMode: ocv1.AvailabilityModeAvailable,
				},
				Status: ocv1.ClusterCatalogStatus{
					URLs: &ocv1.ClusterCatalogURLs{Base: "URL"},
					ResolvedSource: &ocv1.ResolvedCatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ResolvedImageSource{
							Ref: "",
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterCatalogTypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonUnavailable,
						},
						{
							Type:   ocv1.ClusterCatalogTypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterCatalogReasonUserSpecifiedUnavailable,
						},
					},
				},
			},
		},
		{
			name: "reconcile should be short-circuited if the clustercatalog has a deletion timestamp and all known finalizers have been removed",
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "catalog",
					Finalizers:        []string{"finalizer"},
					DeletionTimestamp: &metav1.Time{Time: time.Date(2025, 6, 10, 16, 43, 0, 0, time.UTC)},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
					AvailabilityMode: ocv1.AvailabilityModeAvailable,
				},
			},
			expectedCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "catalog",
					Finalizers:        []string{"finalizer"},
					DeletionTimestamp: &metav1.Time{Time: time.Date(2025, 6, 10, 16, 43, 0, 0, time.UTC)}},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
					AvailabilityMode: ocv1.AvailabilityModeAvailable,
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &ClusterCatalogReconciler{
				Client:         nil,
				ImagePuller:    tt.puller,
				ImageCache:     tt.cache,
				Storage:        tt.store,
				storedCatalogs: map[string]storedCatalogData{},
			}
			if reconciler.ImageCache == nil {
				reconciler.ImageCache = &imageutil.FakeCache{}
			}
			require.NoError(t, reconciler.setupFinalizers())
			ctx := context.Background()

			res, err := reconciler.reconcile(ctx, tt.catalog)
			assert.Equal(t, ctrl.Result{}, res)
			// errors are aggregated/wrapped
			if tt.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			}
			diff := cmp.Diff(tt.expectedCatalog, tt.catalog,
				cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime"),
				cmpopts.SortSlices(func(a, b metav1.Condition) bool { return a.Type < b.Type }))
			assert.Empty(t, diff, "comparing the expected Catalog")
		})
	}
}

func TestPollingRequeue(t *testing.T) {
	for name, tc := range map[string]struct {
		catalog              *ocv1.ClusterCatalog
		expectedRequeueAfter time.Duration
		lastPollTime         time.Time
	}{
		"ClusterCatalog with tag based image ref without any poll interval specified, requeueAfter set to 0, ie polling disabled": {
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
			},
			expectedRequeueAfter: time.Second * 0,
			lastPollTime:         time.Now(),
		},
		"ClusterCatalog with tag based image ref with poll interval specified, just polled, requeueAfter set to wait.jitter(pollInterval)": {
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref:                 "my.org/someimage:latest",
							PollIntervalMinutes: ptr.To(5),
						},
					},
				},
			},
			expectedRequeueAfter: time.Minute * 5,
			lastPollTime:         time.Now(),
		},
		"ClusterCatalog with tag based image ref with poll interval specified, last polled 2m ago, requeueAfter set to wait.jitter(pollInterval-2)": {
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref:                 "my.org/someimage:latest",
							PollIntervalMinutes: ptr.To(5),
						},
					},
				},
			},
			expectedRequeueAfter: time.Minute * 3,
			lastPollTime:         time.Now().Add(-2 * time.Minute),
		},
	} {
		t.Run(name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			ref := mustRef(t, "my.org/someimage@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
			tc.catalog.Status = ocv1.ClusterCatalogStatus{
				Conditions: []metav1.Condition{
					{Type: ocv1.ClusterCatalogTypeServing, Status: metav1.ConditionTrue, Reason: ocv1.ClusterCatalogReasonAvailable, Message: "Serving desired content from resolved source", LastTransitionTime: metav1.Now()},
					{Type: ocv1.ClusterCatalogTypeProgressing, Status: metav1.ConditionTrue, Reason: ocv1.ClusterCatalogReasonSucceeded, Message: "Successfully unpacked and stored content from resolved source", LastTransitionTime: metav1.Now()},
				},
				ResolvedSource: &ocv1.ResolvedCatalogSource{
					Type:  ocv1.SourceTypeImage,
					Image: &ocv1.ResolvedImageSource{Ref: ref.String()},
				},
				URLs:         &ocv1.ClusterCatalogURLs{Base: "URL"},
				LastUnpacked: ptr.To(metav1.NewTime(time.Now().Truncate(time.Second))),
			}
			reconciler := &ClusterCatalogReconciler{
				Client: nil,
				ImagePuller: &imageutil.FakePuller{
					ImageFS: &fstest.MapFS{},
					Ref:     ref,
				},
				Storage: newMockStore(mockCtrl, false),
				storedCatalogs: map[string]storedCatalogData{
					tc.catalog.Name: {
						ref:                ref,
						lastSuccessfulPoll: tc.lastPollTime,
						lastUnpack:         tc.catalog.Status.LastUnpacked.Time,
					},
				},
			}
			require.NoError(t, reconciler.setupFinalizers())
			res, _ := reconciler.reconcile(context.Background(), tc.catalog)
			assert.InDelta(t, tc.expectedRequeueAfter, res.RequeueAfter, 2*requeueJitterMaxFactor*float64(tc.expectedRequeueAfter))
		})
	}
}

func TestPollingReconcilerUnpack(t *testing.T) {
	oldDigest := "a5d4f4467250074216eb1ba1c36e06a3ab797d81c431427fc2aca97ecaf4e9d8"
	newDigest := "f42337e7b85a46d83c94694638e2312e10ca16a03542399a65ba783c94a32b63"

	successfulObservedGeneration := int64(2)
	successfulRef := mustRef(t, "my.org/someimage@sha256:"+oldDigest)
	successfulUnpackTime := time.Time{}
	successfulUnpackStatus := func(mods ...func(status *ocv1.ClusterCatalogStatus)) ocv1.ClusterCatalogStatus {
		s := ocv1.ClusterCatalogStatus{
			URLs: &ocv1.ClusterCatalogURLs{Base: "URL"},
			Conditions: []metav1.Condition{
				{
					Type:               ocv1.ClusterCatalogTypeProgressing,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ClusterCatalogReasonSucceeded,
					Message:            "Successfully unpacked and stored content from resolved source",
					ObservedGeneration: successfulObservedGeneration,
				},
				{
					Type:               ocv1.ClusterCatalogTypeServing,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ClusterCatalogReasonAvailable,
					Message:            "Serving desired content from resolved source",
					ObservedGeneration: successfulObservedGeneration,
				},
			},
			ResolvedSource: &ocv1.ResolvedCatalogSource{
				Type: ocv1.SourceTypeImage,
				Image: &ocv1.ResolvedImageSource{
					Ref: successfulRef.String(),
				},
			},
			LastUnpacked: ptr.To(metav1.NewTime(successfulUnpackTime)),
		}
		for _, mod := range mods {
			mod(&s)
		}
		return s
	}
	successfulStoredCatalogData := func(lastPoll time.Time) map[string]storedCatalogData {
		return map[string]storedCatalogData{
			"test-catalog": {
				observedGeneration: successfulObservedGeneration,
				ref:                successfulRef,
				lastUnpack:         successfulUnpackTime,
				lastSuccessfulPoll: lastPoll,
			},
		}
	}

	for name, tc := range map[string]struct {
		catalog           *ocv1.ClusterCatalog
		storedCatalogData map[string]storedCatalogData
		expectedUnpackRun bool
	}{
		"ClusterCatalog being resolved the first time, unpack should run": {
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-catalog",
					Finalizers: []string{fbcDeletionFinalizer},
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref:                 "my.org/someimage:latest",
							PollIntervalMinutes: ptr.To(5),
						},
					},
				},
			},
			expectedUnpackRun: true,
		},
		"ClusterCatalog not being resolved the first time, no pollInterval mentioned, unpack should not run": {
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-catalog",
					Finalizers: []string{fbcDeletionFinalizer},
					Generation: 2,
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref: "my.org/someimage:latest",
						},
					},
				},
				Status: successfulUnpackStatus(),
			},
			storedCatalogData: successfulStoredCatalogData(time.Now()),
			expectedUnpackRun: false,
		},
		"ClusterCatalog not being resolved the first time, pollInterval mentioned, \"now\" is before next expected poll time, unpack should not run": {
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-catalog",
					Finalizers: []string{fbcDeletionFinalizer},
					Generation: 2,
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref:                 "my.org/someimage:latest",
							PollIntervalMinutes: ptr.To(7),
						},
					},
				},
				Status: successfulUnpackStatus(),
			},
			storedCatalogData: successfulStoredCatalogData(time.Now()),
			expectedUnpackRun: false,
		},
		"ClusterCatalog not being resolved the first time, pollInterval mentioned, \"now\" is after next expected poll time, unpack should run": {
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-catalog",
					Finalizers: []string{fbcDeletionFinalizer},
					Generation: 2,
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref:                 "my.org/someimage:latest",
							PollIntervalMinutes: ptr.To(3),
						},
					},
				},
				Status: successfulUnpackStatus(),
			},
			storedCatalogData: successfulStoredCatalogData(time.Now().Add(-5 * time.Minute)),
			expectedUnpackRun: true,
		},
		"ClusterCatalog not being resolved the first time, pollInterval mentioned, \"now\" is before next expected poll time, generation changed, unpack should run": {
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-catalog",
					Finalizers: []string{fbcDeletionFinalizer},
					Generation: 3,
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref:                 "my.org/someotherimage@sha256:" + newDigest,
							PollIntervalMinutes: ptr.To(7),
						},
					},
				},
				Status: successfulUnpackStatus(),
			},
			storedCatalogData: successfulStoredCatalogData(time.Now()),
			expectedUnpackRun: true,
		},
		"ClusterCatalog not being resolved the first time, no stored catalog in cache, unpack should run": {
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-catalog",
					Finalizers: []string{fbcDeletionFinalizer},
					Generation: 3,
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref:                 "my.org/someotherimage@sha256:" + newDigest,
							PollIntervalMinutes: ptr.To(7),
						},
					},
				},
				Status: successfulUnpackStatus(),
			},
			expectedUnpackRun: true,
		},
		"ClusterCatalog not being resolved the first time, unexpected status, unpack should run": {
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-catalog",
					Finalizers: []string{fbcDeletionFinalizer},
					Generation: 3,
				},
				Spec: ocv1.ClusterCatalogSpec{
					Source: ocv1.CatalogSource{
						Type: ocv1.SourceTypeImage,
						Image: &ocv1.ImageSource{
							Ref:                 "my.org/someotherimage@sha256:" + newDigest,
							PollIntervalMinutes: ptr.To(7),
						},
					},
				},
				Status: successfulUnpackStatus(func(status *ocv1.ClusterCatalogStatus) {
					meta.FindStatusCondition(status.Conditions, ocv1.ClusterCatalogTypeProgressing).Status = metav1.ConditionTrue
				}),
			},
			storedCatalogData: successfulStoredCatalogData(time.Now()),
			expectedUnpackRun: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			scd := tc.storedCatalogData
			if scd == nil {
				scd = map[string]storedCatalogData{}
			}
			reconciler := &ClusterCatalogReconciler{
				Client:         nil,
				ImagePuller:    &imageutil.FakePuller{Error: errors.New("mockpuller error")},
				Storage:        newMockStore(mockCtrl, false),
				storedCatalogs: scd,
			}
			require.NoError(t, reconciler.setupFinalizers())
			_, err := reconciler.reconcile(context.Background(), tc.catalog)
			if tc.expectedUnpackRun {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func mustRef(t *testing.T, ref string) reference.Canonical {
	t.Helper()
	p, err := reference.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	return p.(reference.Canonical)
}

func TestClusterCatalogReconcilerRetiredCatalogs(t *testing.T) {
	const retiredName = "openshift-redhat-marketplace"
	clusterCatalogsResource := ocv1.GroupVersion.WithResource("clustercatalogs").GroupResource()
	unexpectedPull := &imageutil.FakePuller{Error: errors.New("unexpected image pull")}
	successfulPull := &imageutil.FakePuller{
		ImageFS: &fstest.MapFS{},
		Ref:     mustRef(t, "my.org/someimage@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"),
	}
	rejectDelete := func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
		return fmt.Errorf("unexpected delete of %q", obj.GetName())
	}
	// noStorageCalls fails the test if the reconciler touches catalog storage at all.
	noStorageCalls := func(c *gomock.Controller) storage.Instance { return mockstorage.NewMockInstance(c) }
	servingStorage := func(c *gomock.Controller) storage.Instance { return newMockStore(c, false) }

	for _, tc := range []struct {
		name            string
		retiredCatalogs sets.Set[string]
		existing        *ocv1.ClusterCatalog
		request         string
		puller          imageutil.Puller
		store           func(*gomock.Controller) storage.Instance
		// onDelete intercepts client Delete calls; nil passes them through to the fake client.
		onDelete       func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error
		wantErr        string
		wantExists     bool
		wantDeleting   bool
		wantFinalizers []string
		wantServing    bool
	}{
		{
			name:            "retired catalog is deleted without being unpacked and keeps its finalizer for cleanup",
			retiredCatalogs: sets.New(retiredName),
			existing: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{Name: retiredName, Finalizers: []string{fbcDeletionFinalizer}},
				Spec: ocv1.ClusterCatalogSpec{Source: ocv1.CatalogSource{
					Type:  ocv1.SourceTypeImage,
					Image: &ocv1.ImageSource{Ref: "my.org/someimage:latest"},
				}},
			},
			request:        retiredName,
			puller:         unexpectedPull,
			store:          noStorageCalls,
			wantExists:     true,
			wantDeleting:   true,
			wantFinalizers: []string{fbcDeletionFinalizer},
		},
		{
			name:            "retired catalog that is already being deleted is finalized instead of deleted again",
			retiredCatalogs: sets.New(retiredName),
			existing: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name:              retiredName,
					Finalizers:        []string{fbcDeletionFinalizer},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: ocv1.ClusterCatalogSpec{Source: ocv1.CatalogSource{
					Type:  ocv1.SourceTypeImage,
					Image: &ocv1.ImageSource{Ref: "my.org/someimage:latest"},
				}},
			},
			request: retiredName,
			puller:  unexpectedPull,
			store: func(c *gomock.Controller) storage.Instance {
				// The delete-server-cache finalizer must remove the stored content exactly once.
				m := mockstorage.NewMockInstance(c)
				m.EXPECT().Delete(retiredName).Return(nil)
				return m
			},
			onDelete:   rejectDelete,
			wantExists: false,
		},
		{
			name:            "retired catalog removed by someone else before the delete request is not an error",
			retiredCatalogs: sets.New(retiredName),
			existing: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{Name: retiredName},
				Spec: ocv1.ClusterCatalogSpec{Source: ocv1.CatalogSource{
					Type:  ocv1.SourceTypeImage,
					Image: &ocv1.ImageSource{Ref: "my.org/someimage:latest"},
				}},
			},
			request: retiredName,
			puller:  unexpectedPull,
			store:   noStorageCalls,
			onDelete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				if err := c.Delete(ctx, obj, opts...); err != nil {
					return err
				}
				return c.Delete(ctx, obj, opts...)
			},
			wantExists: false,
		},
		{
			name:            "retired catalog that does not exist is ignored",
			retiredCatalogs: sets.New(retiredName),
			request:         retiredName,
			puller:          unexpectedPull,
			store:           noStorageCalls,
			onDelete:        rejectDelete,
			wantExists:      false,
		},
		{
			name:            "failure to delete a retired catalog is returned for retry",
			retiredCatalogs: sets.New(retiredName),
			existing: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{Name: retiredName, Finalizers: []string{fbcDeletionFinalizer}},
				Spec: ocv1.ClusterCatalogSpec{Source: ocv1.CatalogSource{
					Type:  ocv1.SourceTypeImage,
					Image: &ocv1.ImageSource{Ref: "my.org/someimage:latest"},
				}},
			},
			request: retiredName,
			puller:  unexpectedPull,
			store:   noStorageCalls,
			onDelete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
				return apierrors.NewForbidden(clusterCatalogsResource, obj.GetName(), errors.New("denied"))
			},
			wantErr:        `error deleting retired ClusterCatalog "openshift-redhat-marketplace"`,
			wantExists:     true,
			wantFinalizers: []string{fbcDeletionFinalizer},
		},
		{
			name:            "unrelated catalog is reconciled normally",
			retiredCatalogs: sets.New(retiredName),
			existing: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-redhat-operators", Finalizers: []string{fbcDeletionFinalizer}},
				Spec: ocv1.ClusterCatalogSpec{Source: ocv1.CatalogSource{
					Type:  ocv1.SourceTypeImage,
					Image: &ocv1.ImageSource{Ref: "my.org/someimage:latest"},
				}},
			},
			request:        "openshift-redhat-operators",
			puller:         successfulPull,
			store:          servingStorage,
			onDelete:       rejectDelete,
			wantExists:     true,
			wantFinalizers: []string{fbcDeletionFinalizer},
			wantServing:    true,
		},
		{
			name:            "catalog whose name only contains a retired name is reconciled normally",
			retiredCatalogs: sets.New(retiredName),
			existing: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{Name: "openshift-redhat-marketplace-mirror", Finalizers: []string{fbcDeletionFinalizer}},
				Spec: ocv1.ClusterCatalogSpec{Source: ocv1.CatalogSource{
					Type:  ocv1.SourceTypeImage,
					Image: &ocv1.ImageSource{Ref: "my.org/someimage:latest"},
				}},
			},
			request:        "openshift-redhat-marketplace-mirror",
			puller:         successfulPull,
			store:          servingStorage,
			onDelete:       rejectDelete,
			wantExists:     true,
			wantFinalizers: []string{fbcDeletionFinalizer},
			wantServing:    true,
		},
		{
			name:            "catalog is reconciled normally when no catalogs are retired",
			retiredCatalogs: nil,
			existing: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{Name: retiredName, Finalizers: []string{fbcDeletionFinalizer}},
				Spec: ocv1.ClusterCatalogSpec{Source: ocv1.CatalogSource{
					Type:  ocv1.SourceTypeImage,
					Image: &ocv1.ImageSource{Ref: "my.org/someimage:latest"},
				}},
			},
			request:        retiredName,
			puller:         successfulPull,
			store:          servingStorage,
			onDelete:       rejectDelete,
			wantExists:     true,
			wantFinalizers: []string{fbcDeletionFinalizer},
			wantServing:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, ocv1.AddToScheme(scheme))
			builder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&ocv1.ClusterCatalog{}).
				WithInterceptorFuncs(interceptor.Funcs{Delete: tc.onDelete})
			if tc.existing != nil {
				builder = builder.WithObjects(tc.existing)
			}
			cl := builder.Build()

			reconciler := &ClusterCatalogReconciler{
				Client:          cl,
				ImagePuller:     tc.puller,
				ImageCache:      &imageutil.FakeCache{},
				Storage:         tc.store(gomock.NewController(t)),
				RetiredCatalogs: tc.retiredCatalogs,
				storedCatalogs:  map[string]storedCatalogData{},
			}
			require.NoError(t, reconciler.setupFinalizers())

			ctx := context.Background()
			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: tc.request}})
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
			assert.Equal(t, ctrl.Result{}, res)

			got := &ocv1.ClusterCatalog{}
			getErr := cl.Get(ctx, types.NamespacedName{Name: tc.request}, got)
			if !tc.wantExists {
				require.True(t, apierrors.IsNotFound(getErr), "expected ClusterCatalog %q to be gone, got: %v", tc.request, getErr)
				return
			}
			require.NoError(t, getErr)
			assert.Equal(t, tc.wantDeleting, got.GetDeletionTimestamp() != nil, "deletion timestamp")
			assert.Equal(t, tc.wantFinalizers, got.GetFinalizers(), "finalizers")
			assert.Equal(t, tc.wantServing, meta.IsStatusConditionTrue(got.Status.Conditions, ocv1.ClusterCatalogTypeServing), "serving condition")
		})
	}
}
