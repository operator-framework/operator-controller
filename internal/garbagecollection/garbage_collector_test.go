package garbagecollection

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/metadata/fake"

	catalogdv1 "github.com/operator-framework/catalogd/api/v1"
)

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
						APIVersion: catalogdv1.GroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "one",
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterCatalog",
						APIVersion: catalogdv1.GroupVersion.String(),
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
						APIVersion: catalogdv1.GroupVersion.String(),
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

			runtimeObjs := []runtime.Object{}
			for _, catalog := range tt.existCatalogs {
				runtimeObjs = append(runtimeObjs, catalog)
			}

			metaClient := fake.NewSimpleMetadataClient(scheme, runtimeObjs...)

			_, err := runGarbageCollection(ctx, cachePath, metaClient)
			if !tt.wantErr {
				assert.NoError(t, err)
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
