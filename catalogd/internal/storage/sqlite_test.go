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
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestSqliteServerHandler(t *testing.T) {
	store, err := NewSQLiteV1(t.TempDir(), &url.URL{Path: urlPrefix})
	require.NoError(t, err)

	testFS := fstest.MapFS{
		"meta.json": &fstest.MapFile{
			Data: []byte(`{"foo":"bar"}`),
		},
	}
	if store.Store(context.Background(), "test-catalog", testFS) != nil {
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
		// {
		// 	name:               "Server returns 404 when non-existent catalog is queried",
		// 	expectedStatusCode: http.StatusNotFound,
		// 	expectedContent:    "404 Not Found",
		// 	URLPath:            "/catalogs/non-existent-catalog/api/v1/all",
		// },
		{
			name:               "Server returns 200 when path '/catalogs/<catalog>/api/v1/all' is queried, when catalog exists",
			expectedStatusCode: http.StatusOK,
			expectedContent:    `{"foo":"bar"}`,
			URLPath:            "/catalogs/test-catalog/api/v1/all",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s", testServer.URL, tc.URLPath), nil)
			require.NoError(t, err)
			req.Header.Set("Accept-Encoding", "gzip")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			require.Equal(t, tc.expectedStatusCode, resp.StatusCode)

			var actualContent []byte
			actualContent, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, tc.expectedContent, strings.TrimSpace(string(actualContent)))
			require.NoError(t, resp.Body.Close())
		})
	}
}

func TestSqliteServerQueryEndpoint(t *testing.T) {
	store, err := NewSQLiteV1(t.TempDir(), &url.URL{Path: urlPrefix})
	require.NoError(t, err)
	if store.Store(context.Background(), "test-catalog", createTestFS(t)) != nil {
		t.Fatal("failed to store test catalog")
	}
	testServer := httptest.NewServer(store.StorageServerHandler())

	testCases := []struct {
		name               string
		setupStore         func() (*httptest.Server, error)
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
		// {
		// 	name:               "valid query for package schema for a package that does not exist",
		// 	queryParams:        "?schema=olm.package&name=not-present",
		// 	expectedStatusCode: http.StatusOK,
		// 	expectedContent:    "",
		// },
		{
			name:               "valid query with package and name",
			queryParams:        "?package=webhook_operator_test&name=bundle.v0.0.1",
			expectedStatusCode: http.StatusOK,
			expectedContent:    `{"image":"quaydock.io/namespace/bundle:0.0.3","name":"bundle.v0.0.1","package":"webhook_operator_test","properties":[{"type":"olm.bundle.object","value":{"data":"dW5pbXBvcnRhbnQK"}},{"type":"some.other","value":{"data":"arbitrary-info"}}],"relatedImages":[{"image":"testimage:latest","name":"test"}],"schema":"olm.bundle"}`,
		},
		// {
		// 	name:               "invalid query with non-existent schema",
		// 	queryParams:        "?schema=non_existent_schema",
		// 	expectedStatusCode: http.StatusNotFound,
		// 	expectedContent:    "400 Bad Request",
		// },
		// {
		// 	name:               "cached response with If-Modified-Since",
		// 	queryParams:        "?schema=olm.package",
		// 	expectedStatusCode: http.StatusNotModified,
		// 	expectedContent:    "",
		// },
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/catalogs/test-catalog/api/v1/query%s", testServer.URL, tc.queryParams), nil)
			require.NoError(t, err)

			if strings.Contains(tc.name, "If-Modified-Since") {
				// Do an initial request to get a Last-Modified timestamp
				// for the actual request
				resp, err := http.DefaultClient.Do(req)
				require.NoError(t, err)
				req.Header.Set("If-Modified-Since", resp.Header.Get("Last-Modified"))
			}
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, tc.expectedStatusCode, resp.StatusCode)

			actualContent, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, tc.expectedContent, strings.TrimSpace(string(actualContent)))
		})
	}
}

func TestSqliteServerLoadHandling(t *testing.T) {
	store, err := NewSQLiteV1(t.TempDir(), &url.URL{Path: urlPrefix})
	require.NoError(t, err)

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
						fmt.Sprintf("%s/catalogs/test-catalog/api/v1/query?schema=olm.bundle", baseURL),
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
					require.Equal(t, resp.Header.Get("Content-Type"), "application/jsonl")
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
						fmt.Sprintf("%s/catalogs/test-catalog/api/v1/query?package=test-op-%d", baseURL, i),
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
			name:       "mixed all and query endpoints",
			concurrent: 40,
			requests: func(baseURL string) []*http.Request {
				var reqs []*http.Request
				for i := 0; i < 20; i++ {
					allReq, _ := http.NewRequest(http.MethodGet,
						fmt.Sprintf("%s/catalogs/test-catalog/api/v1/all", baseURL),
						nil)
					queryReq, _ := http.NewRequest(http.MethodGet,
						fmt.Sprintf("%s/catalogs/test-catalog/api/v1/query?schema=olm.bundle", baseURL),
						nil)
					allReq.Header.Set("Accept", "application/jsonl")
					queryReq.Header.Set("Accept", "application/jsonl")
					reqs = append(reqs, allReq, queryReq)
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
		"bundle.yaml":  {Data: []byte(testBundle), Mode: os.ModePerm},
		"package.yaml": {Data: []byte(testPackage), Mode: os.ModePerm},
		"channel.yaml": {Data: []byte(testChannel), Mode: os.ModePerm},
	}
}
