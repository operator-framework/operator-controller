package storage

import (
	"context"
	"crypto/x509"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"github.com/operator-framework/rukpak/internal/util"
)

var _ = Describe("HTTP", func() {
	var (
		ctx        context.Context
		bundle     *rukpakv1alpha1.Bundle
		testFS     fs.FS
		localStore *LocalDirectory
		server     *httptest.Server
	)
	BeforeEach(func() {
		ctx = context.Background()
		bundle = &rukpakv1alpha1.Bundle{ObjectMeta: metav1.ObjectMeta{
			Name: util.GenerateBundleName("testbundle", rand.String(8)),
		}}

		// Generate a test FS to represent the bundle.
		testFS = generateFS()

		// Create a test directory to store bundles.
		testDir := filepath.Join(GinkgoT().TempDir(), rand.String(8))
		Expect(os.MkdirAll(testDir, 0700)).To(Succeed())

		// Setup the local store and store the generated FS.
		localStore = &LocalDirectory{RootDirectory: testDir}
		Expect(localStore.Store(ctx, bundle, testFS)).To(Succeed())

		// Create and start the server
		server = newTLSServer(localStore, "abc123")

		// Populate the content URL, this has to happen after the server has
		// started so that we know the server's base URL.
		contentURL, err := localStore.URLFor(ctx, bundle)
		Expect(err).To(BeNil())
		bundle.Status.ContentURL = contentURL
	})
	AfterEach(func() {
		server.Close()
		Expect(os.RemoveAll(localStore.RootDirectory)).To(Succeed())
	})
	Context("with insecure skip verify disabled", func() {
		var opts []HTTPOption
		BeforeEach(func() {
			opts = append(opts, WithInsecureSkipVerify(false))
		})

		It("should get a certificate verification error", func() {
			store := NewHTTP(opts...)
			loadedTestFS, err := store.Load(ctx, bundle)
			Expect(loadedTestFS).To(BeNil())
			Expect(err).To(MatchError(Or(
				ContainSubstring("certificate is not trusted"),              // works on darwin
				ContainSubstring("certificate signed by unknown authority"), // works on linux
			)))
		})
	})
	Context("with insecure skip verify enabled", func() {
		var opts []HTTPOption
		BeforeEach(func() {
			opts = append(opts, WithInsecureSkipVerify(true))
		})
		Context("with correct bearer token", func() {
			BeforeEach(func() {
				opts = append(opts, WithBearerToken("abc123"))
			})
			Context("with existing bundle", func() {
				It("should succeed", func() {
					store := NewHTTP(opts...)
					loadedTestFS, err := store.Load(ctx, bundle)
					Expect(fsEqual(testFS, loadedTestFS)).To(BeTrue())
					Expect(err).To(BeNil())
				})
			})
			Context("with non-existing bundle", func() {
				BeforeEach(func() {
					bundle.Status.ContentURL += "foobar"
				})
				It("should get 404 not found error", func() {
					store := NewHTTP(opts...)
					loadedTestFS, err := store.Load(ctx, bundle)
					Expect(loadedTestFS).To(BeNil())
					Expect(err).To(MatchError(ContainSubstring("404 Not Found")))
				})
			})
		})
		Context("with incorrect bearer token", func() {
			BeforeEach(func() {
				opts = append(opts, WithBearerToken("xyz789"))
			})
			It("should get a 401 Unauthorized error", func() {
				store := NewHTTP(opts...)
				loadedTestFS, err := store.Load(ctx, bundle)
				Expect(loadedTestFS).To(BeNil())
				Expect(err).To(MatchError(ContainSubstring("401 Unauthorized")))
			})
		})
	})
	Context("with a valid root CA chain", func() {
		var opts []HTTPOption
		BeforeEach(func() {
			certPool := x509.NewCertPool()
			certPool.AddCert(server.Certificate())
			opts = append(opts, WithRootCAs(certPool))
		})
		Context("with correct bearer token", func() {
			BeforeEach(func() {
				opts = append(opts, WithBearerToken("abc123"))
			})
			Context("with existing bundle", func() {
				It("should succeed", func() {
					store := NewHTTP(opts...)
					loadedTestFS, err := store.Load(ctx, bundle)
					Expect(fsEqual(testFS, loadedTestFS)).To(BeTrue())
					Expect(err).To(BeNil())
				})
			})
			Context("with non-existing bundle", func() {
				BeforeEach(func() {
					bundle.Status.ContentURL += "foobar"
				})
				It("should get 404 not found error", func() {
					store := NewHTTP(opts...)
					loadedTestFS, err := store.Load(ctx, bundle)
					Expect(loadedTestFS).To(BeNil())
					Expect(err).To(MatchError(ContainSubstring("404 Not Found")))
				})
			})
		})
		Context("with incorrect bearer token", func() {
			BeforeEach(func() {
				opts = append(opts, WithBearerToken("xyz789"))
			})
			It("should get a 401 Unauthorized error", func() {
				store := NewHTTP(opts...)
				loadedTestFS, err := store.Load(ctx, bundle)
				Expect(loadedTestFS).To(BeNil())
				Expect(err).To(MatchError(ContainSubstring("401 Unauthorized")))
			})
		})
	})
})

func newTLSServer(localStore *LocalDirectory, bearerToken string) *httptest.Server {
	server := httptest.NewTLSServer(http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != fmt.Sprintf("Bearer %s", bearerToken) {
			resp.WriteHeader(http.StatusUnauthorized)
			return
		}
		localStore.ServeHTTP(resp, req)
	}))
	localStore.URL = url.URL{
		Scheme: "https",
		Host:   strings.TrimPrefix(server.URL, "https://"),
		Path:   "/",
	}
	return server
}
