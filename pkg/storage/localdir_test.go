package storage

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

const urlPrefix = "/catalogs/"

var ctx = context.Background()

var _ = Describe("LocalDir Storage Test", func() {
	var (
		catalog                     = "test-catalog"
		store                       Instance
		rootDir                     string
		baseURL                     *url.URL
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
		Expect(err).ToNot(HaveOccurred())
		rootDir = d

		baseURL = &url.URL{Scheme: "http", Host: "test-addr", Path: urlPrefix}
		store = LocalDir{RootDir: rootDir, BaseURL: baseURL}
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

			gotConfig, err := declcfg.LoadFS(ctx, unpackResultFS)
			Expect(err).To(Not(HaveOccurred()))
			storedConfig, err := declcfg.LoadFile(os.DirFS(filepath.Join(rootDir, catalog)), "all.json")
			Expect(err).To(Not(HaveOccurred()))
			diff := cmp.Diff(gotConfig, storedConfig)
			Expect(diff).To(Equal(""))
		})
		It("should form the content URL correctly", func() {
			Expect(store.ContentURL(catalog)).To(Equal(fmt.Sprintf("%s%s/all.json", baseURL, catalog)))
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

var _ = Describe("LocalDir Server Handler tests", func() {
	var (
		testServer *httptest.Server
		store      LocalDir
	)
	BeforeEach(func() {
		d, err := os.MkdirTemp(GinkgoT().TempDir(), "cache")
		Expect(err).ToNot(HaveOccurred())
		Expect(os.Mkdir(filepath.Join(d, "test-catalog"), 0700)).To(Succeed())
		store = LocalDir{RootDir: d, BaseURL: &url.URL{Path: urlPrefix}}
		testServer = httptest.NewServer(store.StorageServerHandler())

	})
	It("gets 404 for the path /", func() {
		expectNotFound(testServer.URL)
	})
	It("gets 404 for the path /catalogs/", func() {
		expectNotFound(fmt.Sprintf("%s/%s", testServer.URL, "/catalogs/"))
	})
	It("gets 404 for the path /catalogs/test-catalog/", func() {
		expectNotFound(fmt.Sprintf("%s/%s", testServer.URL, "/catalogs/test-catalog/"))
	})
	It("gets 404 for the path /test-catalog/foo.txt", func() {
		// This ensures that even if the file exists, the URL must contain the /catalogs/ prefix
		Expect(os.WriteFile(filepath.Join(store.RootDir, "test-catalog", "foo.txt"), []byte("bar"), 0600)).To(Succeed())
		expectNotFound(fmt.Sprintf("%s/%s", testServer.URL, "/test-catalog/foo.txt"))
	})
	It("gets 404 for the path /catalogs/test-catalog/non-existent.txt", func() {
		expectNotFound(fmt.Sprintf("%s/%s", testServer.URL, "/catalogs/test-catalog/non-existent.txt"))
	})
	It("gets 200 for the path /catalogs/foo.txt", func() {
		expectedContent := []byte("bar")
		Expect(os.WriteFile(filepath.Join(store.RootDir, "foo.txt"), expectedContent, 0600)).To(Succeed())
		expectFound(fmt.Sprintf("%s/%s", testServer.URL, "/catalogs/foo.txt"), expectedContent)
	})
	It("gets 200 for the path /catalogs/test-catalog/foo.txt", func() {
		expectedContent := []byte("bar")
		Expect(os.WriteFile(filepath.Join(store.RootDir, "test-catalog", "foo.txt"), expectedContent, 0600)).To(Succeed())
		expectFound(fmt.Sprintf("%s/%s", testServer.URL, "/catalogs/test-catalog/foo.txt"), expectedContent)
	})
	AfterEach(func() {
		testServer.Close()
	})
})

func expectNotFound(url string) {
	resp, err := http.Get(url) //nolint:gosec
	Expect(err).To(Not(HaveOccurred()))
	Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	Expect(resp.Body.Close()).To(Succeed())
}

func expectFound(url string, expectedContent []byte) {
	resp, err := http.Get(url) //nolint:gosec
	Expect(err).To(Not(HaveOccurred()))
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	actualContent, err := io.ReadAll(resp.Body)
	Expect(err).To(Not(HaveOccurred()))
	Expect(actualContent).To(Equal(expectedContent))
	Expect(resp.Body.Close()).To(Succeed())
}

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
