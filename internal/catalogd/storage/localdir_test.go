package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

const urlPrefix = "/catalogs/"

func TestLocalDirStoraget(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T) (*LocalDirV1, fs.FS)
		test    func(*testing.T, *LocalDirV1, fs.FS)
		cleanup func(*testing.T, *LocalDirV1)
	}{
		{
			name: "store and retrieve catalog content",
			setup: func(t *testing.T) (*LocalDirV1, fs.FS) {
				s := &LocalDirV1{
					RootDir: t.TempDir(),
					RootURL: &url.URL{Scheme: "http", Host: "test-addr", Path: urlPrefix},
				}
				return s, createTestFS(t)
			},
			test: func(t *testing.T, s *LocalDirV1, fsys fs.FS) {
				const catalog = "test-catalog"

				// Initially content should not exist
				if s.ContentExists(catalog) {
					t.Fatal("content should not exist before store")
				}

				// Store the content
				if err := s.Store(context.Background(), catalog, fsys); err != nil {
					t.Fatal(err)
				}

				// Verify content exists after store
				if !s.ContentExists(catalog) {
					t.Fatal("content should exist after store")
				}

				// Delete the content
				if err := s.Delete(catalog); err != nil {
					t.Fatal(err)
				}

				// Verify content no longer exists
				if s.ContentExists(catalog) {
					t.Fatal("content should not exist after delete")
				}
			},
		},
		{
			name: "storing with metas handler enabled should create indices",
			setup: func(t *testing.T) (*LocalDirV1, fs.FS) {
				s := &LocalDirV1{
					RootDir:            t.TempDir(),
					EnableMetasHandler: true,
				}
				return s, createTestFS(t)
			},
			test: func(t *testing.T, s *LocalDirV1, fsys fs.FS) {
				err := s.Store(context.Background(), "test-catalog", fsys)
				if err != nil {
					t.Fatal(err)
				}

				if !s.ContentExists("test-catalog") {
					t.Error("content should exist after store")
				}

				// Verify index file was created
				indexPath := catalogIndexFilePath(s.catalogDir("test-catalog"))
				if _, err := os.Stat(indexPath); err != nil {
					t.Errorf("index file should exist: %v", err)
				}
			},
		},
		{
			name: "concurrent reads during write should not cause data race",
			setup: func(t *testing.T) (*LocalDirV1, fs.FS) {
				dir := t.TempDir()
				s := &LocalDirV1{RootDir: dir}
				return s, createTestFS(t)
			},
			test: func(t *testing.T, s *LocalDirV1, fsys fs.FS) {
				const catalog = "test-catalog"
				var wg sync.WaitGroup

				// Start multiple concurrent readers
				for i := 0; i < 10; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for j := 0; j < 100; j++ {
							s.ContentExists(catalog)
						}
					}()
				}

				// Write while readers are active
				err := s.Store(context.Background(), catalog, fsys)
				if err != nil {
					t.Fatal(err)
				}

				wg.Wait()
			},
		},
		{
			name: "delete nonexistent catalog",
			setup: func(t *testing.T) (*LocalDirV1, fs.FS) {
				return &LocalDirV1{RootDir: t.TempDir()}, nil
			},
			test: func(t *testing.T, s *LocalDirV1, _ fs.FS) {
				err := s.Delete("nonexistent")
				if err != nil {
					t.Errorf("expected no error deleting nonexistent catalog, got: %v", err)
				}
			},
		},
		{
			name: "store with invalid permissions",
			setup: func(t *testing.T) (*LocalDirV1, fs.FS) {
				dir := t.TempDir()
				// Set directory permissions to deny access
				if err := os.Chmod(dir, 0000); err != nil {
					t.Fatal(err)
				}
				return &LocalDirV1{RootDir: dir}, createTestFS(t)
			},
			test: func(t *testing.T, s *LocalDirV1, fsys fs.FS) {
				err := s.Store(context.Background(), "test-catalog", fsys)
				if !errors.Is(err, fs.ErrPermission) {
					t.Errorf("expected permission error, got: %v", err)
				}
			},
			cleanup: func(t *testing.T, s *LocalDirV1) {
				// Restore permissions so cleanup can succeed
				require.NoError(t, os.Chmod(s.RootDir, 0700))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, fsys := tt.setup(t)
			tt.test(t, s, fsys)
			if tt.cleanup != nil {
				tt.cleanup(t, s)
			}
		})
	}
}

func TestLocalDirServerHandler(t *testing.T) {
	store := &LocalDirV1{RootDir: t.TempDir(), RootURL: &url.URL{Path: urlPrefix}}
	if store.Store(context.Background(), "test-catalog", createTestFS(t)) != nil {
		t.Fatal("failed to store test catalog and start server")
	}

	testServer := httptest.NewServer(store.StorageServerHandler())
	defer testServer.Close()

	for _, tc := range []struct {
		name               string
		URLPath            string
		expectedStatusCode int
		expectedContent    string
	}{
		{
			name:               "Server returns 404 when root URL is queried",
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    "404 page not found",
			URLPath:            "",
		},
		{
			name:               "Server returns 404 when path '/' is queried",
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    "404 page not found",
			URLPath:            "/",
		},
		{
			name:               "Server returns 404 when path '/catalogs/' is queried",
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    "404 page not found",
			URLPath:            "/catalogs/",
		},
		{
			name:               "Server returns 404 when path '/catalogs/<catalog>/' is queried",
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    "404 page not found",
			URLPath:            "/catalogs/test-catalog/",
		},
		{
			name:               "Server returns 404 when path '/catalogs/<catalog>/api/' is queried",
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    "404 page not found",
			URLPath:            "/catalogs/test-catalog/api/",
		},
		{
			name:               "Serer return 404 when path '/catalogs/<catalog>/api/v1' is queried",
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    "404 page not found",
			URLPath:            "/catalogs/test-catalog/api/v1c",
		},
		{
			name:               "Server return 404 when path '/catalogs/<catalog>/non-existent.txt' is queried",
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    "404 page not found",
			URLPath:            "/catalogs/test-catalog/non-existent.txt",
		},
		{
			name:               "Server returns 404 when path '/catalogs/<catalog>.jsonl' is queried even if the file exists, since we don't serve the filesystem, and serve an API instead",
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    "404 page not found",
			URLPath:            "/catalogs/test-catalog.jsonl",
		},
		{
			name:               "Server returns 404 when non-existent catalog is queried",
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    "404 Not Found",
			URLPath:            "/catalogs/non-existent-catalog/api/v1/all",
		},
		{
			name:               "Server returns 200 with json-lines payload when path '/catalogs/<catalog>/api/v1/all' is queried, when catalog exists",
			expectedStatusCode: http.StatusOK,
			expectedContent: `{"image":"quaydock.io/namespace/bundle:0.0.3","name":"bundle.v0.0.1","package":"webhook_operator_test","properties":[{"type":"olm.bundle.object","value":{"data":"dW5pbXBvcnRhbnQK"}},{"type":"some.other","value":{"data":"arbitrary-info"}}],"relatedImages":[{"image":"testimage:latest","name":"test"}],"schema":"olm.bundle"}` + "\n" +
				`{"defaultChannel":"preview_test","name":"webhook_operator_test","schema":"olm.package"}` + "\n" +
				`{"entries":[{"name":"bundle.v0.0.1"}],"name":"preview_test","package":"webhook_operator_test","schema":"olm.channel"}`,
			URLPath: "/catalogs/test-catalog/api/v1/all",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s", testServer.URL, tc.URLPath), nil)
			require.NoError(t, err)
			req.Header.Set("Accept-Encoding", "gzip")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			require.Equal(t, tc.expectedStatusCode, resp.StatusCode)
			if resp.StatusCode == http.StatusOK {
				assert.Equal(t, "application/jsonl", resp.Header.Get("Content-Type"))
			}

			actualContent, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			require.Equal(t, strings.TrimSpace(tc.expectedContent), strings.TrimSpace(string(actualContent)))
			require.NoError(t, resp.Body.Close())
		})
	}
}

// Tests to verify the behavior of the metas endpoint, as described in
// https://docs.google.com/document/d/1s6_9IFEKGQLNh3ueH7SF4Yrx4PW9NSiNFqFIJx0pU-8/
func TestMetasEndpoint(t *testing.T) {
	store := &LocalDirV1{
		RootDir:            t.TempDir(),
		RootURL:            &url.URL{Path: urlPrefix},
		EnableMetasHandler: true,
	}
	if store.Store(context.Background(), "test-catalog", createTestFS(t)) != nil {
		t.Fatal("failed to store test catalog")
	}
	testServer := httptest.NewServer(store.StorageServerHandler())

	testCases := []struct {
		name               string
		initRequest        func(req *http.Request) error
		queryParams        string
		expectedStatusCode int
		expectedContent    string
	}{
		{
			name:               "valid query with package schema",
			queryParams:        "?schema=olm.package",
			expectedStatusCode: http.StatusOK,
			expectedContent:    `{"defaultChannel":"preview_test","name":"webhook_operator_test","schema":"olm.package"}`,
		},
		{
			name:               "valid query with schema and name combination",
			queryParams:        "?schema=olm.package&name=webhook_operator_test",
			expectedStatusCode: http.StatusOK,
			expectedContent:    `{"defaultChannel":"preview_test","name":"webhook_operator_test","schema":"olm.package"}`,
		},
		{
			name:               "valid query with channel schema and package name combination",
			queryParams:        "?schema=olm.channel&package=webhook_operator_test",
			expectedStatusCode: http.StatusOK,
			expectedContent:    `{"entries":[{"name":"bundle.v0.0.1"}],"name":"preview_test","package":"webhook_operator_test","schema":"olm.channel"}`,
		},
		{
			name:               "query with all meta fields",
			queryParams:        "?schema=olm.bundle&package=webhook_operator_test&name=bundle.v0.0.1",
			expectedStatusCode: http.StatusOK,
			expectedContent:    `{"image":"quaydock.io/namespace/bundle:0.0.3","name":"bundle.v0.0.1","package":"webhook_operator_test","properties":[{"type":"olm.bundle.object","value":{"data":"dW5pbXBvcnRhbnQK"}},{"type":"some.other","value":{"data":"arbitrary-info"}}],"relatedImages":[{"image":"testimage:latest","name":"test"}],"schema":"olm.bundle"}`,
		},
		{
			name:               "valid query for package schema for a package that does not exist",
			queryParams:        "?schema=olm.package&name=not-present",
			expectedStatusCode: http.StatusOK,
			expectedContent:    "",
		},
		{
			name:               "valid query with package and name",
			queryParams:        "?package=webhook_operator_test&name=bundle.v0.0.1",
			expectedStatusCode: http.StatusOK,
			expectedContent:    `{"image":"quaydock.io/namespace/bundle:0.0.3","name":"bundle.v0.0.1","package":"webhook_operator_test","properties":[{"type":"olm.bundle.object","value":{"data":"dW5pbXBvcnRhbnQK"}},{"type":"some.other","value":{"data":"arbitrary-info"}}],"relatedImages":[{"image":"testimage:latest","name":"test"}],"schema":"olm.bundle"}`,
		},
		{
			name:               "query with non-existent schema",
			queryParams:        "?schema=non_existent_schema",
			expectedStatusCode: http.StatusOK,
			expectedContent:    "",
		},
		{
			name:               "valid query with packageName that returns multiple blobs in json-lines format",
			queryParams:        "?package=webhook_operator_test",
			expectedStatusCode: http.StatusOK,
			expectedContent: `{"image":"quaydock.io/namespace/bundle:0.0.3","name":"bundle.v0.0.1","package":"webhook_operator_test","properties":[{"type":"olm.bundle.object","value":{"data":"dW5pbXBvcnRhbnQK"}},{"type":"some.other","value":{"data":"arbitrary-info"}}],"relatedImages":[{"image":"testimage:latest","name":"test"}],"schema":"olm.bundle"}
{"entries":[{"name":"bundle.v0.0.1"}],"name":"preview_test","package":"webhook_operator_test","schema":"olm.channel"}`,
		},
		{
			name:        "cached response with If-Modified-Since",
			queryParams: "?schema=olm.package",
			initRequest: func(req *http.Request) error {
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return err
				}
				resp.Body.Close()
				req.Header.Set("If-Modified-Since", resp.Header.Get("Last-Modified"))
				return nil
			},
			expectedStatusCode: http.StatusNotModified,
			expectedContent:    "",
		},
		{
			name:               "request with unknown parameters",
			queryParams:        "?non-existent=foo",
			expectedStatusCode: http.StatusBadRequest,
			expectedContent:    "400 Bad Request",
		},
		{
			name:               "request with duplicate parameters",
			queryParams:        "?schema=olm.bundle&&schema=olm.bundle",
			expectedStatusCode: http.StatusOK,
			expectedContent:    `{"image":"quaydock.io/namespace/bundle:0.0.3","name":"bundle.v0.0.1","package":"webhook_operator_test","properties":[{"type":"olm.bundle.object","value":{"data":"dW5pbXBvcnRhbnQK"}},{"type":"some.other","value":{"data":"arbitrary-info"}}],"relatedImages":[{"image":"testimage:latest","name":"test"}],"schema":"olm.bundle"}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reqGet, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/catalogs/test-catalog/api/v1/metas%s", testServer.URL, tc.queryParams), nil)
			require.NoError(t, err)

			if tc.initRequest != nil {
				require.NoError(t, tc.initRequest(reqGet))
			}
			resp, err := http.DefaultClient.Do(reqGet)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, tc.expectedStatusCode, resp.StatusCode)
			if resp.StatusCode == http.StatusOK {
				assert.Equal(t, "application/jsonl", resp.Header.Get("Content-Type"))
			}

			actualContent, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, tc.expectedContent, strings.TrimSpace(string(actualContent)))

			// Also do a HEAD request
			reqHead, err := http.NewRequest(http.MethodHead, fmt.Sprintf("%s/catalogs/test-catalog/api/v1/metas%s", testServer.URL, tc.queryParams), nil)
			require.NoError(t, err)
			if tc.initRequest != nil {
				require.NoError(t, tc.initRequest(reqHead))
			}
			resp, err = http.DefaultClient.Do(reqHead)
			require.NoError(t, err)
			require.Equal(t, tc.expectedStatusCode, resp.StatusCode)
			actualContent, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Empty(t, string(actualContent)) // HEAD should not return a body
			resp.Body.Close()

			// And make sure any other method is not allowed
			for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
				reqPost, err := http.NewRequest(method, fmt.Sprintf("%s/catalogs/test-catalog/api/v1/metas%s", testServer.URL, tc.queryParams), nil)
				require.NoError(t, err)
				resp, err = http.DefaultClient.Do(reqPost)
				require.NoError(t, err)
				require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
				resp.Body.Close()
			}
		})
	}
}

func TestServerLoadHandling(t *testing.T) {
	store := &LocalDirV1{
		RootDir:            t.TempDir(),
		RootURL:            &url.URL{Path: urlPrefix},
		EnableMetasHandler: true,
	}

	// Create large test data
	largeFS := fstest.MapFS{}
	for i := 0; i < 1000; i++ {
		largeFS[fmt.Sprintf("meta_%d.json", i)] = &fstest.MapFile{
			Data: []byte(fmt.Sprintf(`{"schema":"olm.bundle","package":"test-op-%d","name":"test-op.v%d.0"}`, i, i)),
		}
	}

	if err := store.Store(context.Background(), "test-catalog", largeFS); err != nil {
		t.Fatal("failed to store test catalog")
	}

	testServer := httptest.NewServer(store.StorageServerHandler())
	defer testServer.Close()

	tests := []struct {
		name         string
		concurrent   int
		requests     func(baseURL string) []*http.Request
		validateFunc func(t *testing.T, responses []*http.Response, errs []error)
	}{
		{
			name:       "concurrent identical queries",
			concurrent: 100,
			requests: func(baseURL string) []*http.Request {
				var reqs []*http.Request
				for i := 0; i < 100; i++ {
					req, _ := http.NewRequest(http.MethodGet,
						fmt.Sprintf("%s/catalogs/test-catalog/api/v1/metas?schema=olm.bundle", baseURL),
						nil)
					req.Header.Set("Accept", "application/jsonl")
					reqs = append(reqs, req)
				}
				return reqs
			},
			validateFunc: func(t *testing.T, responses []*http.Response, errs []error) {
				for _, err := range errs {
					require.NoError(t, err)
				}
				for _, resp := range responses {
					require.Equal(t, http.StatusOK, resp.StatusCode)
					require.Equal(t, "application/jsonl", resp.Header.Get("Content-Type"))
					resp.Body.Close()
				}
			},
		},
		{
			name:       "concurrent different queries",
			concurrent: 50,
			requests: func(baseURL string) []*http.Request {
				var reqs []*http.Request
				for i := 0; i < 50; i++ {
					req, _ := http.NewRequest(http.MethodGet,
						fmt.Sprintf("%s/catalogs/test-catalog/api/v1/metas?package=test-op-%d", baseURL, i),
						nil)
					req.Header.Set("Accept", "application/jsonl")
					reqs = append(reqs, req)
				}
				return reqs
			},
			validateFunc: func(t *testing.T, responses []*http.Response, errs []error) {
				for _, err := range errs {
					require.NoError(t, err)
				}
				for _, resp := range responses {
					require.Equal(t, http.StatusOK, resp.StatusCode)
					body, err := io.ReadAll(resp.Body)
					require.NoError(t, err)
					require.Contains(t, string(body), "test-op-")
					resp.Body.Close()
				}
			},
		},
		{
			name:       "mixed all and metas endpoints",
			concurrent: 40,
			requests: func(baseURL string) []*http.Request {
				var reqs []*http.Request
				for i := 0; i < 20; i++ {
					allReq, _ := http.NewRequest(http.MethodGet,
						fmt.Sprintf("%s/catalogs/test-catalog/api/v1/all", baseURL),
						nil)
					metasReq, _ := http.NewRequest(http.MethodGet,
						fmt.Sprintf("%s/catalogs/test-catalog/api/v1/metas?schema=olm.bundle", baseURL),
						nil)
					allReq.Header.Set("Accept", "application/jsonl")
					metasReq.Header.Set("Accept", "application/jsonl")
					reqs = append(reqs, allReq, metasReq)
				}
				return reqs
			},
			validateFunc: func(t *testing.T, responses []*http.Response, errs []error) {
				for _, err := range errs {
					require.NoError(t, err)
				}
				for _, resp := range responses {
					require.Equal(t, http.StatusOK, resp.StatusCode)
					resp.Body.Close()
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				wg        sync.WaitGroup
				responses = make([]*http.Response, tt.concurrent)
				errs      = make([]error, tt.concurrent)
			)

			requests := tt.requests(testServer.URL)
			for i := 0; i < tt.concurrent; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					// nolint:bodyclose
					// the response body is closed in the validateFunc
					resp, err := http.DefaultClient.Do(requests[idx])
					responses[idx] = resp
					errs[idx] = err
				}(i)
			}

			wg.Wait()
			tt.validateFunc(t, responses, errs)
		})
	}
}

func createTestFS(t *testing.T) fs.FS {
	t.Helper()
	testBundleTemplate := `---
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

	testPackageTemplate := `---
defaultChannel: %s
name: %s
schema: olm.package
`

	testChannelTemplate := `---
schema: olm.channel
package: %s
name: %s
entries:
  - name: %s
`
	testBundleName := "bundle.v0.0.1"
	testBundleImage := "quaydock.io/namespace/bundle:0.0.3"
	testBundleRelatedImageName := "test"
	testBundleRelatedImageImage := "testimage:latest"
	testBundleObjectData := "dW5pbXBvcnRhbnQK"
	testPackageDefaultChannel := "preview_test"
	testPackageName := "webhook_operator_test"
	testChannelName := "preview_test"

	testPackage := fmt.Sprintf(testPackageTemplate, testPackageDefaultChannel, testPackageName)
	testBundle := fmt.Sprintf(testBundleTemplate, testBundleImage, testBundleName, testPackageName, testBundleRelatedImageName, testBundleRelatedImageImage, testBundleObjectData)
	testChannel := fmt.Sprintf(testChannelTemplate, testPackageName, testChannelName, testBundleName)
	return &fstest.MapFS{
		"test-catalog.yaml": {Data: []byte(
			generateJSONLinesOrFail(t, []byte(testBundle)) +
				generateJSONLinesOrFail(t, []byte(testPackage)) +
				generateJSONLinesOrFail(t, []byte(testChannel))),
			Mode: os.ModePerm},
	}
}

// generateJSONLinesOrFail takes a byte slice of concatenated JSON objects and returns a JSONlines-formatted string
// or raises a test failure in case of encountering any internal errors
func generateJSONLinesOrFail(t *testing.T, in []byte) string {
	var out strings.Builder
	reader := bytes.NewReader(in)

	err := declcfg.WalkMetasReader(reader, func(meta *declcfg.Meta, err error) error {
		if err != nil {
			return err
		}

		if meta != nil && meta.Blob != nil {
			if meta.Blob[len(meta.Blob)-1] != '\n' {
				return fmt.Errorf("blob does not end with newline")
			}
		}

		_, err = out.Write(meta.Blob)
		if err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	return out.String()
}
