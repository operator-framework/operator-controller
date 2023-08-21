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

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/action"

	"github.com/operator-framework/operator-controller/internal/resolution/entitysources"
)

type indexRefEntitySource struct {
	renderer      action.Render
	entitiesCache input.EntityList
}

func newIndexRefEntitySourceEntitySource(indexRef string) *indexRefEntitySource {
	return &indexRefEntitySource{
		renderer: action.Render{
			Refs:           []string{indexRef},
			AllowedRefMask: action.RefDCImage | action.RefDCDir,
		},
	}
}

func (es *indexRefEntitySource) Get(_ context.Context, _ deppy.Identifier) (*input.Entity, error) {
	panic("not implemented")
}

func (es *indexRefEntitySource) Filter(ctx context.Context, filter input.Predicate) (input.EntityList, error) {
	entities, err := es.entities(ctx)
	if err != nil {
		return nil, err
	}

	resultSet := input.EntityList{}
	for i := range entities {
		if filter(&entities[i]) {
			resultSet = append(resultSet, entities[i])
		}
	}
	return resultSet, nil
}

func (es *indexRefEntitySource) GroupBy(ctx context.Context, fn input.GroupByFunction) (input.EntityListMap, error) {
	entities, err := es.entities(ctx)
	if err != nil {
		return nil, err
	}

	resultSet := input.EntityListMap{}
	for i := range entities {
		keys := fn(&entities[i])
		for _, key := range keys {
			resultSet[key] = append(resultSet[key], entities[i])
		}
	}
	return resultSet, nil
}

func (es *indexRefEntitySource) Iterate(ctx context.Context, fn input.IteratorFunction) error {
	entities, err := es.entities(ctx)
	if err != nil {
		return err
	}

	for i := range entities {
		if err := fn(&entities[i]); err != nil {
			return err
		}
	}
	return nil
}

func (es *indexRefEntitySource) entities(ctx context.Context) (input.EntityList, error) {
	if es.entitiesCache == nil {
		cfg, err := es.renderer.Run(ctx)
		if err != nil {
			return nil, err
		}

		// TODO: update empty catalog name string to be catalog name once we support multiple catalogs in CLI
		entities, err := entitysources.MetadataToEntities("", cfg.Channels, cfg.Bundles)
		if err != nil {
			return nil, err
		}

		es.entitiesCache = entities
	}

	return es.entitiesCache, nil
}
