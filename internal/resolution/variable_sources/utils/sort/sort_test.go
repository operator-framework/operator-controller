package sort_test

import (
	"sort"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	sort2 "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/utils/sort"
	"github.com/operator-framework/operator-registry/alpha/property"
)

func TestSort(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sort Suite")
}

var _ = Describe("Sort", func() {
	Describe("ByChannelAndVersion", func() {
		It("should order entities by package name", func() {
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
				return sort2.ByChannelAndVersion(entities[i], entities[j])
			})

			Expect(entities[0]).To(Equal(e1))
			Expect(entities[1]).To(Equal(e2))
			Expect(entities[2]).To(Equal(e3))
		})

		It("should order entities by channel name", func() {
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
				return sort2.ByChannelAndVersion(entities[i], entities[j])
			})

			Expect(entities[0]).To(Equal(e1))
			Expect(entities[1]).To(Equal(e2))
			Expect(entities[2]).To(Equal(e3))
		})

		It("should order entities by version number (highest first)", func() {
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
				return sort2.ByChannelAndVersion(entities[i], entities[j])
			})

			Expect(entities[0]).To(Equal(e3))
			Expect(entities[1]).To(Equal(e2))
			Expect(entities[2]).To(Equal(e1))
		})

		It("should order entities by version number (highest first) and channel priority (lower value -> higher priority)", func() {
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
				return sort2.ByChannelAndVersion(entities[i], entities[j])
			})

			Expect(entities[0]).To(Equal(e2))
			Expect(entities[1]).To(Equal(e4))
			Expect(entities[2]).To(Equal(e3))
			Expect(entities[3]).To(Equal(e1))
		})

		It("should order entities missing a property after those that have it", func() {
			e1 := input.NewEntity("test1", map[string]string{
				property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})
			e2 := input.NewEntity("test2", map[string]string{
				property.TypePackage: `{"packageName": "mypackage"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})
			e3 := input.NewEntity("test3", map[string]string{
				property.TypePackage: `{"packageName": "mypackage", "version": "3.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})
			e4 := input.NewEntity("test4", map[string]string{})
			entities := []*input.Entity{e2, e3, e1, e4}

			sort.Slice(entities, func(i, j int) bool {
				return sort2.ByChannelAndVersion(entities[i], entities[j])
			})

			Expect(entities[0]).To(Equal(e3))
			Expect(entities[1]).To(Equal(e1))
			Expect(entities[2]).To(Equal(e2))
			Expect(entities[3]).To(Equal(e4))
		})
	})

})
