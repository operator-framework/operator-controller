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

// index is an index of sections of an FBC file used to lookup FBC blobs that
// match any combination of their schema, package, and name fields.

// This index strikes a balance between space and performance. It indexes each field
// separately, and performs logical set intersections at lookup time in order to implement
// a multi-parameter query.
//
// Note: it is permissible to change the indexing algorithm later if it is necessary to
// tune the space / performance tradeoff. However care should be taken to ensure
// that the actual content returned by the index remains identical, as users of the index
// may be sensitive to differences introduced by index algorithm changes (e.g. if the
// order of the returned sections changes).
type index struct {
	BySchema  map[string][]section `json:"by_schema"`
	ByPackage map[string][]section `json:"by_package"`
	ByName    map[string][]section `json:"by_name"`
}

// A section is the byte offset and length of an FBC blob within the file.
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

func (i index) Get(r io.ReaderAt, schema, packageName, name string) io.Reader {
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
	return io.MultiReader(srs...)
}

func (i *index) getSectionSet(schema, packageName, name string) sets.Set[section] {
	// Initialize with all sections if no schema specified, otherwise use schema sections
	sectionSet := sets.New[section]()
	if schema == "" {
		for _, s := range i.BySchema {
			sectionSet.Insert(s...)
		}
	} else {
		sectionSet = sets.New[section](i.BySchema[schema]...)
	}

	// Filter by package name if specified
	if packageName != "" {
		packageSections := sets.New[section](i.ByPackage[packageName]...)
		sectionSet = sectionSet.Intersection(packageSections)
	}

	// Filter by name if specified
	if name != "" {
		nameSections := sets.New[section](i.ByName[name]...)
		sectionSet = sectionSet.Intersection(nameSections)
	}

	return sectionSet
}

func newIndex(metasChan <-chan *declcfg.Meta) *index {
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
	return idx
}
