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

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
)

type indexRefClient struct {
	renderer     action.Render
	bundlesCache []*catalogmetadata.Bundle
}

func newIndexRefClient(indexRef string) *indexRefClient {
	return &indexRefClient{
		renderer: action.Render{
			Refs:           []string{indexRef},
			AllowedRefMask: action.RefDCImage | action.RefDCDir,
		},
	}
}

func (c *indexRefClient) Bundles(ctx context.Context) ([]*catalogmetadata.Bundle, error) {
	if c.bundlesCache == nil {
		cfg, err := c.renderer.Run(ctx)
		if err != nil {
			return nil, err
		}

		var (
			channels []*catalogmetadata.Channel
			bundles  []*catalogmetadata.Bundle
		)

		for i := range cfg.Channels {
			channels = append(channels, &catalogmetadata.Channel{
				Channel: cfg.Channels[i],
			})
		}

		for i := range cfg.Bundles {
			bundles = append(bundles, &catalogmetadata.Bundle{
				Bundle: cfg.Bundles[i],
			})
		}

		// TODO: update fake catalog name string to be catalog name once we support multiple catalogs in CLI
		catalogName := "offline-catalog"

		bundles, err = client.PopulateExtraFields(catalogName, channels, bundles)
		if err != nil {
			return nil, err
		}

		c.bundlesCache = bundles
	}

	return c.bundlesCache, nil
}
