package source

import (
	"archive/tar"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainersImage_applyLayerFilter(t *testing.T) {
	type testCase struct {
		name       string
		srcPaths   []string
		tarHeaders []tar.Header
		assertion  func(*tar.Header, bool, error)
	}
	for _, tc := range []testCase{
		{
			name:     "everything found when srcPaths represent root",
			srcPaths: []string{"", "/"},
			tarHeaders: []tar.Header{
				{
					Name: "file",
				},
				{
					Name: "/file",
				},
				{
					Name: "/nested/file",
				},
				{
					Name: "/deeply/nested/file",
				},
			},
			assertion: func(tarHeader *tar.Header, keep bool, err error) {
				assert.True(t, keep)
				assert.NoError(t, err)
			},
		},
		{
			name:     "nothing found outside of srcPath",
			srcPaths: []string{"source"},
			tarHeaders: []tar.Header{
				{
					Name: "elsewhere",
				},
				{
					Name: "/elsewhere",
				},
				{
					Name: "/nested/elsewhere",
				},
				{
					Name: "/deeply/nested/elsewhere",
				},
			},
			assertion: func(tarHeader *tar.Header, keep bool, err error) {
				assert.False(t, keep)
				assert.NoError(t, err)
			},
		},
		{
			name:     "absolute paths are trimmed",
			srcPaths: []string{"source", "/source"},
			tarHeaders: []tar.Header{
				{
					Name: "source",
				},
				{
					Name: "/source",
				},
				{
					Name: "source/nested/elsewhere",
				},
				{
					Name: "/source/nested/elsewhere",
				},
				{
					Name: "source/deeply/nested/elsewhere",
				},
				{
					Name: "/source/deeply/nested/elsewhere",
				},
			},
			assertion: func(tarHeader *tar.Header, keep bool, err error) {
				assert.True(t, keep)
				assert.NoError(t, err)
			},
		},
		{
			name:     "up level source paths are not supported",
			srcPaths: []string{"../not-supported"},
			tarHeaders: []tar.Header{
				{
					Name: "anything",
				},
			},
			assertion: func(tarHeader *tar.Header, keep bool, err error) {
				assert.False(t, keep)
				assert.ErrorContains(t, err, "error getting relative path")
			},
		},
		{
			name:     "up level tar headers are not supported",
			srcPaths: []string{"fine"},
			tarHeaders: []tar.Header{
				{
					Name: "../not-supported",
				},
				{
					Name: "../fine",
				},
			},
			assertion: func(tarHeader *tar.Header, keep bool, err error) {
				assert.False(t, keep)
				assert.NoError(t, err)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, srcPath := range tc.srcPaths {
				f := applyLayerFilter(srcPath)
				for _, tarHeader := range tc.tarHeaders {
					keep, err := f(&tarHeader)
					tc.assertion(&tarHeader, keep, err)
				}
			}
		})
	}
}
