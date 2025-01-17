package storage

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

type index struct {
	BySchema  map[string][]section `json:"by_schema"`
	ByPackage map[string][]section `json:"by_package"`
	ByName    map[string][]section `json:"by_name"`
}

type section struct {
	offset int64
	length int64
}

func (s *section) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`[%d,%d]`, s.offset, s.length)), nil
}

func (s *section) UnmarshalJSON(b []byte) error {
	vals := [2]int64{}
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	s.offset = vals[0]
	s.length = vals[1]
	return nil
}

func (i index) Size() int64 {
	size := 0
	for k, v := range i.BySchema {
		size += len(k) + len(v)*16
	}
	for k, v := range i.ByPackage {
		size += len(k) + len(v)*16
	}
	for k, v := range i.ByName {
		size += len(k) + len(v)*16
	}
	return int64(size)
}

func (i index) Get(r io.ReaderAt, schema, packageName, name string) (io.Reader, bool) {
	sectionSet := i.getSectionSet(schema, packageName, name)

	sections := sectionSet.UnsortedList()
	slices.SortFunc(sections, func(a, b section) int {
		return cmp.Compare(a.offset, b.offset)
	})

	srs := make([]io.Reader, 0, len(sections))
	for _, s := range sections {
		sr := io.NewSectionReader(r, s.offset, s.length)
		srs = append(srs, sr)
	}
	return io.MultiReader(srs...), true
}

func (i *index) getSectionSet(schema, packageName, name string) sets.Set[section] {
	if schema == "" {
		if packageName == "" {
			if name == "" {
				sectionSet := sets.New[section]()
				for _, s := range i.BySchema {
					sectionSet.Insert(s...)
				}
				return sectionSet
			} else {
				return sets.New[section](i.ByName[name]...)
			}
		} else {
			sectionSet := sets.New[section](i.ByPackage[packageName]...)
			if name == "" {
				return sectionSet
			} else {
				return sectionSet.Intersection(sets.New[section](i.ByName[name]...))
			}
		}
	} else {
		sectionSet := sets.New[section](i.BySchema[schema]...)
		if packageName == "" {
			if name == "" {
				return sectionSet
			} else {
				return sectionSet.Intersection(sets.New[section](i.ByName[name]...))
			}
		} else {
			sectionSet = sectionSet.Intersection(sets.New[section](i.ByPackage[packageName]...))
			if name == "" {
				return sectionSet
			} else {
				return sectionSet.Intersection(sets.New[section](i.ByName[name]...))
			}
		}
	}
}

func newIndex(metasChan <-chan *declcfg.Meta) (*index, error) {
	idx := &index{
		BySchema:  make(map[string][]section),
		ByPackage: make(map[string][]section),
		ByName:    make(map[string][]section),
	}
	offset := int64(0)
	for meta := range metasChan {
		start := offset
		length := int64(len(meta.Blob))
		offset += length

		s := section{offset: start, length: length}
		if meta.Schema != "" {
			idx.BySchema[meta.Schema] = append(idx.BySchema[meta.Schema], s)
		}
		if meta.Package != "" {
			idx.ByPackage[meta.Package] = append(idx.ByPackage[meta.Package], s)
		}
		if meta.Name != "" {
			idx.ByName[meta.Name] = append(idx.ByName[meta.Name], s)
		}
	}
	return idx, nil
}
