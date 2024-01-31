/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"

	"github.com/operator-framework/operator-registry/alpha/action"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	"github.com/operator-framework/operator-controller/internal/controllers"
)

type indexRefClient struct {
	renderer      action.Render
	bundlesCache  []*catalogmetadata.Bundle
	channelsCache []*catalogmetadata.Channel
	packagesCache []*catalogmetadata.Package
}

var _ controllers.CatalogProvider = &indexRefClient{}

func newIndexRefClient(indexRef string) *indexRefClient {
	return &indexRefClient{
		renderer: action.Render{
			Refs:           []string{indexRef},
			AllowedRefMask: action.RefDCImage | action.RefDCDir,
		},
	}
}

func (c *indexRefClient) CatalogContents(ctx context.Context) (*client.Contents, error) {
	if c.bundlesCache == nil || c.channelsCache == nil || c.packagesCache == nil {
		cfg, err := c.renderer.Run(ctx)
		if err != nil {
			return nil, err
		}

		var (
			channels []*catalogmetadata.Channel
			bundles  []*catalogmetadata.Bundle
			packages []*catalogmetadata.Package
		)

		// TODO: update fake catalog name string to be catalog name once we support multiple catalogs in CLI
		catalogName := "offline-catalog"

		for i := range cfg.Packages {
			packages = append(packages, &catalogmetadata.Package{
				Package: cfg.Packages[i],
				Catalog: catalogName,
			})
		}

		for i := range cfg.Channels {
			channels = append(channels, &catalogmetadata.Channel{
				Channel: cfg.Channels[i],
				Catalog: catalogName,
			})
		}

		for i := range cfg.Bundles {
			bundles = append(bundles, &catalogmetadata.Bundle{
				Bundle:  cfg.Bundles[i],
				Catalog: catalogName,
			})
		}

		for _, deprecation := range cfg.Deprecations {
			for _, entry := range deprecation.Entries {
				switch entry.Reference.Schema {
				case declcfg.SchemaPackage:
					for _, pkg := range packages {
						if pkg.Name == deprecation.Package {
							pkg.Deprecation = &declcfg.DeprecationEntry{
								Reference: entry.Reference,
								Message:   entry.Message,
							}
						}
					}
				case declcfg.SchemaChannel:
					for _, channel := range channels {
						if channel.Package == deprecation.Package && channel.Name == entry.Reference.Name {
							channel.Deprecation = &declcfg.DeprecationEntry{
								Reference: entry.Reference,
								Message:   entry.Message,
							}
						}
					}
				case declcfg.SchemaBundle:
					for _, bundle := range bundles {
						if bundle.Package == deprecation.Package && bundle.Name == entry.Reference.Name {
							bundle.Deprecation = &declcfg.DeprecationEntry{
								Reference: entry.Reference,
								Message:   entry.Message,
							}
						}
					}
				}
			}
		}

		c.bundlesCache = bundles
		c.channelsCache = channels
		c.packagesCache = packages
	}

	return &client.Contents{}, nil
}
