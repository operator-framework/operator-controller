package entitycache

import (
	"context"
	"reflect"
	"sync"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

type EntityCache struct {
	mutex sync.RWMutex
	cache map[string]map[deppy.Identifier]*input.Entity
}

func NewEntityCache() *EntityCache {
	return &EntityCache{
		mutex: sync.RWMutex{},
		cache: map[string]map[deppy.Identifier]*input.Entity{},
	}
}

func (c *EntityCache) UpdateCache(sourceID string, entities []*input.Entity) bool {
	newSourceCache := make(map[deppy.Identifier]*input.Entity)
	for _, entity := range entities {
		newSourceCache[entity.Identifier()] = entity
	}
	if _, ok := c.cache[sourceID]; ok && reflect.DeepEqual(c.cache[sourceID], newSourceCache) {
		return false
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.cache[sourceID] = newSourceCache
	// return whether cache had updates
	return true
}

func (c *EntityCache) DropSource(sourceID string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.cache, sourceID)
}

func (c *EntityCache) Get(ctx context.Context, id deppy.Identifier) *input.Entity {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	// don't count on deppy ID to reflect its catalogsource
	for _, source := range c.cache {
		if entity, ok := source[id]; ok {
			return entity
		}
	}
	return nil
}

func (c *EntityCache) Filter(ctx context.Context, filter input.Predicate) (input.EntityList, error) {
	resultSet := input.EntityList{}
	if err := c.Iterate(ctx, func(entity *input.Entity) error {
		if filter(entity) {
			resultSet = append(resultSet, *entity)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return resultSet, nil
}

func (c *EntityCache) GroupBy(ctx context.Context, fn input.GroupByFunction) (input.EntityListMap, error) {
	resultSet := input.EntityListMap{}
	if err := c.Iterate(ctx, func(entity *input.Entity) error {
		keys := fn(entity)
		for _, key := range keys {
			resultSet[key] = append(resultSet[key], *entity)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return resultSet, nil
}

func (c *EntityCache) Iterate(ctx context.Context, fn input.IteratorFunction) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	for _, source := range c.cache {
		for _, entity := range source {
			if err := fn(entity); err != nil {
				return err
			}
		}
	}
	return nil
}
