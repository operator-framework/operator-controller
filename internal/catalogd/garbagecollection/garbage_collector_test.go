package garbagecollection

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/metadata/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// TestGarbageCollectorStoragePath verifies that the GarbageCollector cleans up
// orphaned temporary directories in the StoragePath (e.g. /var/cache/catalogs).
// These dirs are created by LocalDirV1.Store during catalog unpacking and normally
// removed by a deferred RemoveAll, but can persist if the process is killed.
func TestGarbageCollectorStoragePath(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, metav1.AddMetaToScheme(scheme))

	storagePath := t.TempDir()

	// Known catalog — its directory must be preserved.
	existingCatalog := &metav1.PartialObjectMetadata{
		TypeMeta:   metav1.TypeMeta{Kind: "ClusterCatalog", APIVersion: ocv1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: "openshift-redhat-operators"},
	}
	require.NoError(t, os.MkdirAll(filepath.Join(storagePath, existingCatalog.Name), 0700))

	// Orphaned temp dirs left by a previously interrupted Store — must be removed.
	for _, orphan := range []string{
		".openshift-redhat-operators-4015104162",
		".openshift-redhat-operators-3615668944",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(storagePath, orphan), 0700))
	}

	metaClient := fake.NewSimpleMetadataClient(scheme, existingCatalog)

	gc := &GarbageCollector{
		CachePaths:     []string{t.TempDir(), storagePath},
		Logger:         logr.Discard(),
		MetadataClient: metaClient,
		Interval:       0,
	}
	gc.runAndLog(ctx)

	entries, err := os.ReadDir(storagePath)
	require.NoError(t, err)

	// Only the real catalog dir should remain.
	require.Len(t, entries, 1)
	assert.Equal(t, existingCatalog.Name, entries[0].Name())
}

func TestRunGarbageCollection(t *testing.T) {
	for _, tt := range []struct {
		name             string
		existCatalogs    []*metav1.PartialObjectMetadata
		notExistCatalogs []*metav1.PartialObjectMetadata
		wantErr          bool
	}{
		{
			name: "successful garbage collection",
			existCatalogs: []*metav1.PartialObjectMetadata{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterCatalog",
						APIVersion: ocv1.GroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "one",
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterCatalog",
						APIVersion: ocv1.GroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "two",
					},
				},
			},
			notExistCatalogs: []*metav1.PartialObjectMetadata{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterCatalog",
						APIVersion: ocv1.GroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "three",
					},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cachePath := t.TempDir()
			scheme := runtime.NewScheme()
			require.NoError(t, metav1.AddMetaToScheme(scheme))

			allCatalogs := append(tt.existCatalogs, tt.notExistCatalogs...)
			for _, catalog := range allCatalogs {
				require.NoError(t, os.MkdirAll(filepath.Join(cachePath, catalog.Name, "fakedigest"), os.ModePerm))
			}

			runtimeObjs := make([]runtime.Object, 0, len(tt.existCatalogs))
			for _, catalog := range tt.existCatalogs {
				runtimeObjs = append(runtimeObjs, catalog)
			}

			metaClient := fake.NewSimpleMetadataClient(scheme, runtimeObjs...)

			_, err := runGarbageCollection(ctx, cachePath, metaClient)
			if !tt.wantErr {
				require.NoError(t, err)
				entries, err := os.ReadDir(cachePath)
				require.NoError(t, err)
				assert.Len(t, entries, len(tt.existCatalogs))
				for _, catalog := range tt.existCatalogs {
					assert.DirExists(t, filepath.Join(cachePath, catalog.Name))
				}

				for _, catalog := range tt.notExistCatalogs {
					assert.NoDirExists(t, filepath.Join(cachePath, catalog.Name))
				}
			} else {
				assert.Error(t, err)
			}
		})
	}
}
