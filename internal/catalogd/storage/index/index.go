package index

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"os"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

type Index struct {
	idx *index
}

func (i *Index) Get(r io.ReaderAt, schema, packageName, name string) io.Reader {
	return i.idx.get(r, schema, packageName, name)
}

func New(ctx context.Context, metas iter.Seq2[*declcfg.Meta, error]) (*Index, error) {
	idx, err := newIndex(ctx, metas)
	if err != nil {
		return nil, err
	}
	return &Index{idx: idx}, nil
}

func ReadFile(indexFilePath string) (*Index, error) {
	indexFile, err := os.Open(indexFilePath)
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(indexFile)
	var idx index
	if err := dec.Decode(&idx); err != nil {
		return nil, err
	}
	return &Index{idx: &idx}, nil
}

var _ io.WriterTo = (*Index)(nil)

func (i *Index) WriteTo(w io.Writer) (int64, error) {
	data, err := json.Marshal(i.idx)
	if err != nil {
		return -1, err
	}
	written, err := w.Write(data)
	return int64(written), err
}

type index struct {
	BySchema  map[string][]section `json:"by_schema"`
	ByPackage map[string][]section `json:"by_package"`
	ByName    map[string][]section `json:"by_name"`
}

func newIndex(ctx context.Context, metas iter.Seq2[*declcfg.Meta, error]) (*index, error) {
	idx := &index{
		BySchema:  make(map[string][]section),
		ByPackage: make(map[string][]section),
		ByName:    make(map[string][]section),
	}
	offset := int64(0)
	for meta, iterErr := range metas {
		if iterErr != nil {
			return nil, iterErr
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
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

func (i *index) get(r io.ReaderAt, schema, packageName, name string) io.Reader {
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
