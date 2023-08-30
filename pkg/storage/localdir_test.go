package storage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

var _ = Describe("LocalDir Storage Test", func() {
	var (
		catalog                     = "test-catalog"
		store                       Instance
		rootDir                     string
		testBundleName              = "bundle.v0.0.1"
		testBundleImage             = "quaydock.io/namespace/bundle:0.0.3"
		testBundleRelatedImageName  = "test"
		testBundleRelatedImageImage = "testimage:latest"
		testBundleObjectData        = "dW5pbXBvcnRhbnQK"
		testPackageDefaultChannel   = "preview_test"
		testPackageName             = "webhook_operator_test"
		testChannelName             = "preview_test"
		testPackage                 = fmt.Sprintf(testPackageTemplate, testPackageDefaultChannel, testPackageName)
		testBundle                  = fmt.Sprintf(testBundleTemplate, testBundleImage, testBundleName, testPackageName, testBundleRelatedImageName, testBundleRelatedImageImage, testBundleObjectData)
		testChannel                 = fmt.Sprintf(testChannelTemplate, testPackageName, testChannelName, testBundleName)

		unpackResultFS fs.FS
	)
	BeforeEach(func() {
		d, err := os.MkdirTemp(GinkgoT().TempDir(), "cache")
		rootDir = d
		Expect(err).ToNot(HaveOccurred())

		store = LocalDir{RootDir: rootDir}
		unpackResultFS = &fstest.MapFS{
			"bundle.yaml":  &fstest.MapFile{Data: []byte(testBundle), Mode: os.ModePerm},
			"package.yaml": &fstest.MapFile{Data: []byte(testPackage), Mode: os.ModePerm},
			"channel.yaml": &fstest.MapFile{Data: []byte(testChannel), Mode: os.ModePerm},
		}
	})
	When("An unpacked FBC is stored using LocalDir", func() {
		BeforeEach(func() {
			err := store.Store(catalog, unpackResultFS)
			Expect(err).To(Not(HaveOccurred()))
		})
		It("should store the content in the RootDir correctly", func() {
			fbcFile := filepath.Join(rootDir, catalog, "all.json")
			_, err := os.Stat(fbcFile)
			Expect(err).To(Not(HaveOccurred()))

			gotConfig, err := declcfg.LoadFS(unpackResultFS)
			Expect(err).To(Not(HaveOccurred()))
			storedConfig, err := declcfg.LoadFile(os.DirFS(filepath.Join(rootDir, catalog)), "all.json")
			Expect(err).To(Not(HaveOccurred()))
			diff := cmp.Diff(gotConfig, storedConfig)
			Expect(diff).To(Equal(""))
		})
		When("The stored content is deleted", func() {
			BeforeEach(func() {
				err := store.Delete(catalog)
				Expect(err).To(Not(HaveOccurred()))
			})
			It("should delete the FBC from the cache directory", func() {
				fbcFile := filepath.Join(rootDir, catalog)
				_, err := os.Stat(fbcFile)
				Expect(err).To(HaveOccurred())
				Expect(os.IsNotExist(err)).To(BeTrue())
			})
		})
	})
})

const testBundleTemplate = `---
image: %s
name: %s
schema: olm.bundle
package: %s
relatedImages:
  - name: %s
    image: %s
properties:
  - type: olm.bundle.object
    value:
      data: %s
  - type: some.other
    value:
      data: arbitrary-info
`

const testPackageTemplate = `---
defaultChannel: %s
name: %s
schema: olm.package
`

const testChannelTemplate = `---
schema: olm.channel
package: %s
name: %s
entries:
  - name: %s
`
