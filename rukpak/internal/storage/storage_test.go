package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"github.com/operator-framework/rukpak/internal/util"
)

var _ = Describe("WithFallbackLoader", func() {
	var (
		ctx            context.Context
		primaryBundle  *rukpakv1alpha1.Bundle
		fallbackBundle *rukpakv1alpha1.Bundle
		primaryStore   *LocalDirectory
		fallbackStore  *LocalDirectory
		primaryFS      fs.FS
		fallbackFS     fs.FS

		store Storage
	)

	BeforeEach(func() {
		ctx = context.Background()
		primaryBundle = &rukpakv1alpha1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name: util.GenerateBundleName("primary", rand.String(8)),
			},
		}
		fallbackBundle = &rukpakv1alpha1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name: util.GenerateBundleName("fallback", rand.String(8)),
			},
		}
		primaryDir := filepath.Join(GinkgoT().TempDir(), fmt.Sprintf("primary-%s", rand.String(8)))
		Expect(os.MkdirAll(primaryDir, 0700)).To(Succeed())
		fallbackDir := filepath.Join(GinkgoT().TempDir(), fmt.Sprintf("fallback-%s", rand.String(8)))
		Expect(os.MkdirAll(fallbackDir, 0700)).To(Succeed())

		primaryStore = &LocalDirectory{RootDirectory: primaryDir}
		primaryFS = generateFS()
		Expect(primaryStore.Store(ctx, primaryBundle, primaryFS)).To(Succeed())

		fallbackStore = &LocalDirectory{RootDirectory: fallbackDir}
		fallbackFS = generateFS()
		Expect(fallbackStore.Store(ctx, fallbackBundle, fallbackFS)).To(Succeed())

		store = WithFallbackLoader(primaryStore, fallbackStore)
	})

	It("should find primary bundle", func() {
		loadedTestFS, err := store.Load(ctx, primaryBundle)
		Expect(err).To(BeNil())
		Expect(fsEqual(primaryFS, loadedTestFS))
	})
	It("should find fallback bundle", func() {
		loadedTestFS, err := store.Load(ctx, fallbackBundle)
		Expect(err).To(BeNil())
		Expect(fsEqual(fallbackFS, loadedTestFS))
	})
	It("should fail to find unknown bundle", func() {
		unknownBundle := &rukpakv1alpha1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name: util.GenerateBundleName("unknown", rand.String(8)),
			},
		}
		loadedTestFS, err := store.Load(ctx, unknownBundle)
		Expect(err).To(WithTransform(func(err error) bool { return errors.Is(err, os.ErrNotExist) }, BeTrue()))
		Expect(loadedTestFS).To(BeNil())
	})
})
