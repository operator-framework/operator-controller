package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing/fstest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

var _ = Describe("LocalDirectory", func() {
	var (
		ctx    context.Context
		owner  *rukpakv1alpha1.Bundle
		store  LocalDirectory
		testFS fs.FS
	)

	BeforeEach(func() {
		ctx = context.Background()
		owner = &rukpakv1alpha1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("test-bundle-%s", rand.String(5)),
				UID:  types.UID(rand.String(8)),
			},
		}
		store = LocalDirectory{RootDirectory: GinkgoT().TempDir()}
		testFS = generateFS()
	})
	When("a bundle is not stored", func() {
		Describe("Store", func() {
			It("should store a bundle FS", func() {
				Expect(store.Store(ctx, owner, testFS)).To(Succeed())
				_, err := os.Stat(filepath.Join(store.RootDirectory, fmt.Sprintf("%s.tgz", owner.GetName())))
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Describe("Load", func() {
			It("should fail due to file not existing", func() {
				_, err := store.Load(ctx, owner)
				Expect(err).To(WithTransform(func(err error) bool { return errors.Is(err, os.ErrNotExist) }, BeTrue()))
			})
		})

		Describe("Delete", func() {
			It("should succeed despite file not existing", func() {
				Expect(store.Delete(ctx, owner)).To(Succeed())
			})
		})
	})
	When("a bundle is stored", func() {
		BeforeEach(func() {
			Expect(store.Store(ctx, owner, testFS)).To(Succeed())
		})
		Describe("Store", func() {
			It("should re-store a bundle FS", func() {
				Expect(store.Store(ctx, owner, testFS)).To(Succeed())
			})
		})

		Describe("Load", func() {
			It("should load the bundle", func() {
				loadedTestFS, err := store.Load(ctx, owner)
				Expect(err).NotTo(HaveOccurred())
				Expect(fsEqual(testFS, loadedTestFS)).To(BeTrue())
			})
		})

		Describe("Delete", func() {
			It("should delete the bundle", func() {
				Expect(store.Delete(ctx, owner)).To(Succeed())
				_, err := os.Stat(filepath.Join(store.RootDirectory, fmt.Sprintf("%s.tgz", owner.GetName())))
				Expect(err).To(WithTransform(func(err error) bool { return errors.Is(err, os.ErrNotExist) }, BeTrue()))
			})
		})
	})
})

func generateFS() fs.FS {
	gen := fstest.MapFS{}

	numFiles := rand.IntnRange(10, 20)
	for i := 0; i < numFiles; i++ {
		pathLength := rand.IntnRange(30, 60)
		filePath := ""
		for j := 0; j < pathLength; j += rand.IntnRange(5, 10) {
			filePath = filepath.Join(filePath, rand.String(rand.IntnRange(5, 10)))
		}
		gen[filePath] = &fstest.MapFile{
			Data: []byte(rand.String(rand.IntnRange(1, 400))),
			Mode: fs.FileMode(rand.IntnRange(0600, 0777)),
			// Need to do some rounding and location shenanigans here to align with nuances of the tar implementation.
			ModTime: time.Now().Round(time.Second).Add(time.Duration(-rand.IntnRange(0, 100000)) * time.Second).In(&time.Location{}),
		}
	}
	return &gen
}

func fsEqual(a, b fs.FS) (bool, error) {
	aMap := fstest.MapFS{}
	bMap := fstest.MapFS{}

	walkFunc := func(f fs.FS, m fstest.MapFS) fs.WalkDirFunc {
		return func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, err := fs.ReadFile(f, path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			m[path] = &fstest.MapFile{
				Data:    data,
				Mode:    d.Type(),
				ModTime: info.ModTime().UTC(),
			}
			return nil
		}
	}
	if err := fs.WalkDir(a, ".", walkFunc(a, aMap)); err != nil {
		return false, err
	}
	if err := fs.WalkDir(b, ".", walkFunc(b, bMap)); err != nil {
		return false, err
	}
	return reflect.DeepEqual(aMap, bMap), nil
}
