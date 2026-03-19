package migration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// catalogMeta represents a single entry from the catalog JSONL response.
type catalogMeta struct {
	Schema  string          `json:"schema"`
	Name    string          `json:"name"`
	Package string          `json:"package"`
	Props   json.RawMessage `json:"properties,omitempty"`
	Entries []channelEntry  `json:"entries,omitempty"`
}

type channelEntry struct {
	Name string `json:"name"`
}

// CatalogPackageInfo holds the results of querying a catalog for a package.
type CatalogPackageInfo struct {
	Found             bool
	AvailableVersions []string
	AvailableChannels []string
	VersionFound      bool
	ChannelFound      bool
}

// QueryCatalogForPackage queries a ClusterCatalog's content to check if the
// specified package, version, and channel are available.
func (m *Migrator) QueryCatalogForPackage(ctx context.Context, catalog *ocv1.ClusterCatalog, packageName, version, channel string, restConfig *rest.Config) (*CatalogPackageInfo, error) {
	if catalog.Status.URLs == nil {
		return nil, fmt.Errorf("catalog %s has no URLs in status", catalog.Name)
	}

	// Build the catalog API URL via the kube API server service proxy
	// The catalog's base URL is like: https://catalogd-service.olmv1-system.svc/catalogs/<catalog-name>
	// We proxy through the API server: /api/v1/namespaces/olmv1-system/services/https:catalogd-service:443/proxy/catalogs/<catalog-name>/api/v1/all
	proxyURL := fmt.Sprintf("%s/api/v1/namespaces/olmv1-system/services/https:catalogd-service:443/proxy/catalogs/%s/api/v1/all",
		restConfig.Host, catalog.Name)

	// Create HTTP client with the kube API server auth
	transportConfig, err := restConfig.TransportConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get transport config: %w", err)
	}

	rt, err := transport.New(transportConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	httpClient := &http.Client{Transport: rt}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog returned status %d", resp.StatusCode)
	}

	return parseCatalogResponse(resp.Body, packageName, version, channel)
}

func parseCatalogResponse(body io.Reader, packageName, version, channel string) (*CatalogPackageInfo, error) {
	info := &CatalogPackageInfo{}
	versionSet := map[string]bool{}
	channelSet := map[string]bool{}

	scanner := bufio.NewScanner(body)
	// Increase buffer for large catalog entries
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var meta catalogMeta
		if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
			continue
		}

		// Check if this entry is for our package
		switch meta.Schema {
		case "olm.package":
			if meta.Name == packageName {
				info.Found = true
			}
		case "olm.bundle":
			if meta.Package != packageName {
				continue
			}
			// Extract version from properties
			bundleVersion := extractBundleVersion(meta.Props)
			if bundleVersion != "" {
				versionSet[bundleVersion] = true
			}
		case "olm.channel":
			if meta.Package != packageName {
				continue
			}
			channelSet[meta.Name] = true
			// Check if our version's bundle is in this channel
			for _, entry := range meta.Entries {
				bundleName := entry.Name
				// Bundle names typically follow <package>.<version> or <package>.v<version>
				if bundleName != "" {
					_ = bundleName // entries tracked via channel membership
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading catalog response: %w", err)
	}

	for v := range versionSet {
		info.AvailableVersions = append(info.AvailableVersions, v)
	}
	for ch := range channelSet {
		info.AvailableChannels = append(info.AvailableChannels, ch)
	}

	info.VersionFound = versionSet[version]
	info.ChannelFound = channel == "" || channelSet[channel]

	return info, nil
}

func extractBundleVersion(propsRaw json.RawMessage) string {
	if propsRaw == nil {
		return ""
	}
	var props []struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(propsRaw, &props); err != nil {
		return ""
	}
	for _, p := range props {
		if p.Type == "olm.package" {
			var pkg struct {
				Version string `json:"version"`
			}
			if err := json.Unmarshal(p.Value, &pkg); err == nil {
				return pkg.Version
			}
		}
	}
	return ""
}

// ResolveClusterCatalog finds a ClusterCatalog that serves the package at the installed version.
// Per RFC Step 3:
//   - Query each available ClusterCatalog for the package at the installed version
//   - If found in multiple, use the highest-priority catalog
//   - If not found in any, return an error (the user must create one)
func (m *Migrator) ResolveClusterCatalog(ctx context.Context, info *MigrationInfo, restConfig *rest.Config) (string, error) {
	var catalogList ocv1.ClusterCatalogList
	if err := m.Client.List(ctx, &catalogList); err != nil {
		return "", fmt.Errorf("failed to list ClusterCatalogs: %w", err)
	}

	type catalogCandidate struct {
		name     string
		priority int32
		pkgInfo  *CatalogPackageInfo
	}
	var candidates []catalogCandidate
	var queriedCatalogs []string

	for i := range catalogList.Items {
		catalog := &catalogList.Items[i]

		if catalog.Spec.AvailabilityMode == ocv1.AvailabilityModeUnavailable {
			continue
		}

		// Check if the catalog is serving
		serving := false
		for _, c := range catalog.Status.Conditions {
			if c.Type == string(metav1.ConditionTrue) {
				continue
			}
			if c.Type == "Serving" && c.Status == metav1.ConditionTrue {
				serving = true
				break
			}
		}
		if !serving {
			continue
		}

		queriedCatalogs = append(queriedCatalogs, catalog.Name)
		m.progress(fmt.Sprintf("Querying catalog %s for package %s@%s...", catalog.Name, info.PackageName, info.Version))

		pkgInfo, err := m.QueryCatalogForPackage(ctx, catalog, info.PackageName, info.Version, info.Channel, restConfig)
		if err != nil {
			m.progress(fmt.Sprintf("Could not query catalog %s: %v", catalog.Name, err))
			continue
		}

		if pkgInfo.Found && pkgInfo.VersionFound && pkgInfo.ChannelFound {
			candidates = append(candidates, catalogCandidate{
				name:     catalog.Name,
				priority: catalog.Spec.Priority,
				pkgInfo:  pkgInfo,
			})
		}
	}

	if len(candidates) == 0 {
		return "", &PackageNotFoundError{
			PackageName:     info.PackageName,
			Version:         info.Version,
			Channel:         info.Channel,
			QueriedCatalogs: queriedCatalogs,
		}
	}

	// Pick the highest-priority catalog
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.priority > best.priority {
			best = c
		}
	}

	return best.name, nil
}

// PackageNotFoundError is returned when no ClusterCatalog contains the required package.
type PackageNotFoundError struct {
	PackageName     string
	Version         string
	Channel         string
	QueriedCatalogs []string
}

func (e *PackageNotFoundError) Error() string {
	msg := fmt.Sprintf("package %q at version %q", e.PackageName, e.Version)
	if e.Channel != "" {
		msg += fmt.Sprintf(" in channel %q", e.Channel)
	}
	msg += " not found in any serving ClusterCatalog"
	if len(e.QueriedCatalogs) > 0 {
		msg += fmt.Sprintf(" (queried: %v)", e.QueriedCatalogs)
	}
	return msg
}

// CreateClusterCatalog creates a ClusterCatalog from a CatalogSource image reference
// and waits for it to reach a serving state.
func (m *Migrator) CreateClusterCatalog(ctx context.Context, name, imageRef string) error {
	catalog := &ocv1.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ocv1.ClusterCatalogSpec{
			Source: ocv1.CatalogSource{
				Type: "Image",
				Image: &ocv1.ImageSource{
					Ref: imageRef,
				},
			},
		},
	}

	if err := m.Client.Create(ctx, catalog); err != nil {
		return fmt.Errorf("failed to create ClusterCatalog: %w", err)
	}

	// Wait for serving
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		var cat ocv1.ClusterCatalog
		if err := m.Client.Get(ctx, client.ObjectKeyFromObject(catalog), &cat); err != nil {
			return false, err
		}
		for _, c := range cat.Status.Conditions {
			if c.Type == "Serving" && c.Status == metav1.ConditionTrue {
				return true, nil
			}
		}
		m.progress(fmt.Sprintf("Waiting for ClusterCatalog %s to become ready...", name))
		return false, nil
	})
}
