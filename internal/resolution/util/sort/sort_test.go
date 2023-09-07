package sort_test

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	entitysort "github.com/operator-framework/operator-controller/internal/resolution/util/sort"
)

func TestSortByPackageName(t *testing.T) {
	e1 := input.NewEntity("test1", map[string]string{
		property.TypePackage: `{"packageName": "package1", "version": "1.0.0"}`,
	})
	e2 := input.NewEntity("test2", map[string]string{
		property.TypePackage: `{"packageName": "package2", "version": "1.0.0"}`,
	})
	e3 := input.NewEntity("test3", map[string]string{
		property.TypePackage: `{"packageName": "package3", "version": "1.0.0"}`,
	})
	entities := []*input.Entity{e2, e3, e1}

	sort.Slice(entities, func(i, j int) bool {
		return entitysort.ByChannelAndVersion(entities[i], entities[j])
	})

	if diff := cmp.Diff(entities[0], e1); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[1], e2); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[2], e3); diff != "" {
		t.Error(diff)
	}
}

func TestSortByChannelName(t *testing.T) {
	e1 := input.NewEntity("test1", map[string]string{
		property.TypePackage: `{"packageName": "package", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stableA","priority":0}`,
	})
	e2 := input.NewEntity("test2", map[string]string{
		property.TypePackage: `{"packageName": "package", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stableB","priority":0}`,
	})
	e3 := input.NewEntity("test3", map[string]string{
		property.TypePackage: `{"packageName": "package", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stableC","priority":0}`,
	})
	entities := []*input.Entity{e2, e3, e1}

	sort.Slice(entities, func(i, j int) bool {
		return entitysort.ByChannelAndVersion(entities[i], entities[j])
	})

	if diff := cmp.Diff(entities[0], e1); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[1], e2); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[2], e3); diff != "" {
		t.Error(diff)
	}
}

func TestSortByVersionNumber(t *testing.T) {
	e1 := input.NewEntity("test1", map[string]string{
		property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
	})
	e2 := input.NewEntity("test2", map[string]string{
		property.TypePackage: `{"packageName": "mypackage", "version": "2.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
	})
	e3 := input.NewEntity("test3", map[string]string{
		property.TypePackage: `{"packageName": "mypackage", "version": "3.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
	})
	entities := []*input.Entity{e2, e3, e1}

	sort.Slice(entities, func(i, j int) bool {
		return entitysort.ByChannelAndVersion(entities[i], entities[j])
	})

	if diff := cmp.Diff(entities[0], e3); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[1], e2); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[2], e1); diff != "" {
		t.Error(diff)
	}
}

func TestSortByVersionNumberAndChannelPriority(t *testing.T) {
	e1 := input.NewEntity("test1", map[string]string{
		property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"beta","priority":1}`,
	})
	e2 := input.NewEntity("test2", map[string]string{
		property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
	})
	e3 := input.NewEntity("test3", map[string]string{
		property.TypePackage: `{"packageName": "mypackage", "version": "2.0.0"}`,
		property.TypeChannel: `{"channelName":"beta","priority":1}`,
	})
	e4 := input.NewEntity("test4", map[string]string{
		property.TypePackage: `{"packageName": "mypackage", "version": "3.0.0"}`,
		property.TypeChannel: `{"channelName":"beta","priority":1}`,
	})
	entities := []*input.Entity{e2, e3, e1, e4}

	sort.Slice(entities, func(i, j int) bool {
		return entitysort.ByChannelAndVersion(entities[i], entities[j])
	})

	if diff := cmp.Diff(entities[0], e2); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[1], e4); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[2], e3); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[3], e1); diff != "" {
		t.Error(diff)
	}
}

func TestSortMissingProperty(t *testing.T) {
	e1 := input.NewEntity("test1", map[string]string{
		property.TypePackage: `{"packageName": "mypackageA", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
	})
	e2 := input.NewEntity("test2", map[string]string{
		property.TypePackage: `{"packageName": "mypackageA"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
	})
	e3 := input.NewEntity("test3", map[string]string{
		property.TypePackage: `{"packageName": "mypackageA", "version": "3.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
	})
	e4 := input.NewEntity("test4", map[string]string{
		property.TypePackage: `{"packageName": "mypackageB", "version": "3.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
	})
	e5 := input.NewEntity("test5", map[string]string{})
	entities := []*input.Entity{e2, e3, e1, e4, e5}

	sort.Slice(entities, func(i, j int) bool {
		return entitysort.ByChannelAndVersion(entities[i], entities[j])
	})
	if diff := cmp.Diff(entities[0], e3); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[1], e1); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[2], e4); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[3], e2); diff != "" { // no version
		t.Error(diff)
	}
	if diff := cmp.Diff(entities[4], e5); diff != "" { // no package - or anything
		t.Error(diff)
	}
}
