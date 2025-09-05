package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/docker/reference"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
)

var _ storage.Instance = &MockStore{}

type MockStore struct {
	shouldError bool
}

func (m MockStore) Store(_ context.Context, _ string, _ fs.FS) error {
	if m.shouldError {
		return errors.New("mockstore store error")
	}
	return nil
}

func (m MockStore) Delete(_ string) error {
	if m.shouldError {
		return errors.New("mockstore delete error")
	}
	return nil
}

func (m MockStore) BaseURL(_ string) string {
	return "URL"
}

func (m MockStore) StorageServerHandler() http.Handler {
	panic("not needed")
}

func (m MockStore) ContentExists(_ string) bool {
	return true
}

func TestCatalogdControllerReconcile(t *testing.T) {
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
			puller: &imageutil.MockPuller{},
			store:  &MockStore{},
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
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonBlocked,
						},
					},
				},
			},
		},
		{
			name:          "valid source type, unpack returns error, status updated to reflect error state and error is returned",
			expectedError: fmt.Errorf("source catalog content: %w", fmt.Errorf("mockpuller error")),
			puller: &imageutil.MockPuller{
				Error: errors.New("mockpuller error"),
			},
			store: &MockStore{},
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
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonRetrying,
						},
					},
				},
			},
		},
		{
			name:          "valid source type, unpack returns terminal error, status updated to reflect terminal error state(Blocked) and error is returned",
			expectedError: fmt.Errorf("source catalog content: %w", reconcile.TerminalError(fmt.Errorf("mockpuller terminal error"))),
			puller: &imageutil.MockPuller{
				Error: reconcile.TerminalError(errors.New("mockpuller terminal error")),
			},
			store: &MockStore{},
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
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonBlocked,
						},
					},
				},
			},
		},
		{
			name: "valid source type, unpack state == Unpacked, should reflect in status that it's progressing, and is serving",
			puller: &imageutil.MockPuller{
				ImageFS: &fstest.MapFS{},
				Ref:     mustRef(t, "my.org/someimage@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"),
			},
			store: &MockStore{},
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
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonAvailable,
						},
						{
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonSucceeded,
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
			puller: &imageutil.MockPuller{
				ImageFS: &fstest.MapFS{},
			},
			store: &MockStore{
				shouldError: true,
			},
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
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonRetrying,
						},
					},
				},
			},
		},
		{
			name: "storage finalizer not set, storage finalizer gets set",
			puller: &imageutil.MockPuller{
				ImageFS: &fstest.MapFS{},
			},
			store: &MockStore{},
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
			puller: &imageutil.MockPuller{
				ImageFS: &fstest.MapFS{},
			},
			store: &MockStore{},
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
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonAvailable,
						},
						{
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonSucceeded,
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
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonUnavailable,
						},
						{
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonSucceeded,
						},
					},
				},
			},
		},
		{
			name:          "storage finalizer set, catalog deletion timestamp is not zero (or nil), storage delete failed, error returned, finalizer not removed and catalog continues serving",
			expectedError: fmt.Errorf("finalizer %q failed: %w", fbcDeletionFinalizer, fmt.Errorf("mockstore delete error")),
			puller: &imageutil.MockPuller{
				ImageFS: &fstest.MapFS{},
			},
			store: &MockStore{
				shouldError: true,
			},
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
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonSucceeded,
						},
						{
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonAvailable,
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
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonRetrying,
						},
						{
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonAvailable,
						},
					},
				},
			},
		},
		{
			name:          "storage finalizer set, catalog deletion timestamp is not zero (or nil), unpack cleanup failed, error returned, finalizer not removed but catalog stops serving",
			expectedError: fmt.Errorf("finalizer %q failed: %w", fbcDeletionFinalizer, fmt.Errorf("mockcache delete error")),
			puller:        &imageutil.MockPuller{},
			cache:         &imageutil.MockCache{DeleteErr: fmt.Errorf("mockcache delete error")},
			store: &MockStore{
				shouldError: false,
			},
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
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonSucceeded,
						},
						{
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonAvailable,
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
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonRetrying,
						},
						{
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonUnavailable,
						},
					},
				},
			},
		},
		{
			name: "catalog availability set to disabled, status.urls should get unset",
			puller: &imageutil.MockPuller{
				ImageFS: &fstest.MapFS{},
			},
			store: &MockStore{},
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
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonAvailable,
						},
						{
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonSucceeded,
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
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonUserSpecifiedUnavailable,
						},
						{
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonSucceeded,
						},
					},
				},
			},
		},
		{
			name: "catalog availability set to disabled, finalizer should get removed",
			puller: &imageutil.MockPuller{
				ImageFS: &fstest.MapFS{},
			},
			store: &MockStore{},
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
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonAvailable,
						},
						{
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonSucceeded,
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
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonUserSpecifiedUnavailable,
						},
						{
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionTrue,
							Reason: ocv1.ReasonSucceeded,
						},
					},
				},
			},
		},
		{
			name: "after catalog availability set to enable, finalizer should be added",
			puller: &imageutil.MockPuller{
				ImageFS: &fstest.MapFS{},
			},
			store: &MockStore{},
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
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonUnavailable,
						},
						{
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonUserSpecifiedUnavailable,
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
							Type:   ocv1.TypeServing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonUnavailable,
						},
						{
							Type:   ocv1.TypeProgressing,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ReasonUserSpecifiedUnavailable,
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
				reconciler.ImageCache = &imageutil.MockCache{}
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
			ref := mustRef(t, "my.org/someimage@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
			tc.catalog.Status = ocv1.ClusterCatalogStatus{
				Conditions: []metav1.Condition{
					{Type: ocv1.TypeServing, Status: metav1.ConditionTrue, Reason: ocv1.ReasonAvailable, Message: "Serving desired content from resolved source", LastTransitionTime: metav1.Now()},
					{Type: ocv1.TypeProgressing, Status: metav1.ConditionTrue, Reason: ocv1.ReasonSucceeded, Message: "Successfully unpacked and stored content from resolved source", LastTransitionTime: metav1.Now()},
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
				ImagePuller: &imageutil.MockPuller{
					ImageFS: &fstest.MapFS{},
					Ref:     ref,
				},
				Storage: &MockStore{},
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
					Type:               ocv1.TypeProgressing,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ReasonSucceeded,
					Message:            "Successfully unpacked and stored content from resolved source",
					ObservedGeneration: successfulObservedGeneration,
				},
				{
					Type:               ocv1.TypeServing,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ReasonAvailable,
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
					meta.FindStatusCondition(status.Conditions, ocv1.TypeProgressing).Status = metav1.ConditionTrue
				}),
			},
			storedCatalogData: successfulStoredCatalogData(time.Now()),
			expectedUnpackRun: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			scd := tc.storedCatalogData
			if scd == nil {
				scd = map[string]storedCatalogData{}
			}
			reconciler := &ClusterCatalogReconciler{
				Client:         nil,
				ImagePuller:    &imageutil.MockPuller{Error: errors.New("mockpuller error")},
				Storage:        &MockStore{},
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
