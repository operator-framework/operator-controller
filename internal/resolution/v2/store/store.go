package store

import (
	"context"
	"encoding/json"

	masterminds "github.com/Masterminds/semver/v3"
	catalogdv1alpha1 "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Store interface {
	GetCatalogs(context.Context) ([]Catalog, error)
	GetInstalledBundles(context.Context) (map[string]*InstalledBundle, error)
	GetCluster(context.Context) (*Cluster, error)
}

type InstalledBundle struct {
	Package string
	Name    string
	Version *masterminds.Version
}

func (c CatalogMetadataStore) GetInstalledBundles(ctx context.Context) (map[string]*InstalledBundle, error) {
	var bdList rukpakv1alpha1.BundleDeploymentList
	if err := c.List(ctx, &bdList); err != nil {
		return nil, err
	}

	bundles := make(map[string]*InstalledBundle, len(bdList.Items))
	for _, bd := range bdList.Items {
		pkg := bd.Annotations["operators.operatorframework.io/package"]
		name := bd.Annotations["operators.operatorframework.io/name"]
		version := bd.Annotations["operators.operatorframework.io/version"]
		if pkg == "" || name == "" || version == "" {
			continue
		}
		vers, err := masterminds.StrictNewVersion(version)
		if err != nil {
			return nil, err
		}
		bundles[pkg] = &InstalledBundle{
			Package: pkg,
			Name:    name,
			Version: vers,
		}
	}
	return bundles, nil
}

type Cluster struct {
	Nodes       []corev1.Node
	VersionInfo *version.Info
}

func (c CatalogMetadataStore) GetCluster(ctx context.Context) (*Cluster, error) {
	var nodeList corev1.NodeList
	if err := c.List(ctx, &nodeList); err != nil {
		return nil, err
	}

	serverVersion, err := c.ServerVersion()
	if err != nil {
		return nil, err
	}

	return &Cluster{
		Nodes:       nodeList.Items,
		VersionInfo: serverVersion,
	}, nil
}

type CatalogMetadataStore struct {
	client.Client
	discovery.ServerVersionInterface
}

func (c CatalogMetadataStore) GetCatalogs(ctx context.Context) ([]Catalog, error) {
	var catalogList catalogdv1alpha1.CatalogList
	if err := c.List(ctx, &catalogList); err != nil {
		return nil, err
	}

	catalogs := make([]Catalog, 0, len(catalogList.Items))
	for _, catalog := range catalogList.Items {
		packages, err := c.getPackages(ctx, catalog.Name)
		if err != nil {
			return nil, err
		}
		for _, pkg := range packages {
			bundles, err := c.getBundles(ctx, catalog.Name, pkg.Name)
			if err != nil {
				return nil, err
			}
			pkg.Bundles = bundles

			channels, err := c.getChannels(ctx, catalog.Name, pkg.Name, bundles)
			if err != nil {
				return nil, err
			}
			pkg.Channels = channels
		}
		catalogs = append(catalogs, Catalog{
			Name:     catalog.Name,
			Packages: packages,
		})
	}
	return catalogs, nil
}

func (c CatalogMetadataStore) getPackages(ctx context.Context, catalogName string) (map[string]*Package, error) {
	var cmList catalogdv1alpha1.CatalogMetadataList
	if err := c.List(ctx, &cmList, client.MatchingLabels{"catalog": catalogName, "schema": declcfg.SchemaPackage}); err != nil {
		return nil, err
	}

	packages := map[string]*Package{}
	for _, cm := range cmList.Items {
		var p declcfg.Package
		if err := json.Unmarshal(cm.Spec.Content, &p); err != nil {
			return nil, err
		}
		packages[p.Name] = &Package{
			CatalogName: catalogName,
			Package:     p,
			Channels:    map[string]*Channel{},
		}
	}
	return packages, nil
}

func (c CatalogMetadataStore) getChannels(ctx context.Context, catalogName string, packageName string, bundles map[string]*Bundle) (map[string]*Channel, error) {
	var cmList catalogdv1alpha1.CatalogMetadataList
	if err := c.List(ctx, &cmList, client.MatchingLabels{"catalog": catalogName, "schema": declcfg.SchemaChannel, "package": packageName}); err != nil {
		return nil, err
	}

	channels := map[string]*Channel{}
	for _, cm := range cmList.Items {
		var ch declcfg.Channel
		if err := json.Unmarshal(cm.Spec.Content, &ch); err != nil {
			return nil, err
		}
		cch, err := NewChannel(catalogName, ch, bundles)
		if err != nil {
			return nil, err
		}
		channels[ch.Name] = cch
	}
	return channels, nil
}

func (c CatalogMetadataStore) getBundles(ctx context.Context, catalogName string, packageName string) (map[string]*Bundle, error) {
	var cmList catalogdv1alpha1.CatalogMetadataList
	if err := c.List(ctx, &cmList, client.MatchingLabels{"catalog": catalogName, "schema": declcfg.SchemaBundle, "package": packageName}); err != nil {
		return nil, err
	}

	bundles := map[string]*Bundle{}
	for _, cm := range cmList.Items {
		var b declcfg.Bundle
		if err := json.Unmarshal(cm.Spec.Content, &b); err != nil {
			return nil, err
		}
		cb, err := NewBundle(catalogName, b)
		if err != nil {
			return nil, err
		}
		bundles[b.Name] = cb
	}
	return bundles, nil
}
