package storage

import (
	"context"
	"errors"
	"iter"
	"maps"
	"slices"
	"sync"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// Instance is a storage instance that stores FBC content of catalogs
// added to a cluster. It can be used to Store or Delete FBC in the
// host's filesystem, and check if data exists for a catalog.
type Instance interface {
	Store(ctx context.Context, catalog string, seq iter.Seq2[*declcfg.Meta, error]) error
	Delete(ctx context.Context, catalog string) error
	Exists(catalog string) bool
}

var _ Instance = (*Instances)(nil)

type Instances struct {
	files          *files
	indices        *indices
	graphQLSchemas *graphQLSchemas
}

type InstancesOption func(i *Instances)

func WithFiles(enabled bool, rootDir string) InstancesOption {
	return func(i *Instances) {
		if enabled {
			i.files = newFiles(rootDir)
		}
	}
}

func WithIndices(enabled bool, rootDir string) InstancesOption {
	return func(i *Instances) {
		if enabled {
			i.indices = newIndices(rootDir)
		}
	}
}

func WithGraphQLSchemas(enabled bool) InstancesOption {
	return func(i *Instances) {
		if enabled {
			i.graphQLSchemas = newGraphQLSchemas()
		}
	}
}

func NewInstances(opts ...InstancesOption) *Instances {
	i := &Instances{}
	for _, opt := range opts {
		opt(i)
	}
	return i
}

func (i *Instances) Files() Files {
	if i.files == nil {
		panic("files data was not initialized")
	}
	return i.files
}

func (i *Instances) Indices() Indices {
	if i.indices == nil {
		panic("indices data was not initialized")
	}
	return i.indices
}

func (i *Instances) GraphQLSchemas() GraphQLSchemas {
	if i.graphQLSchemas == nil {
		panic("graphQLSchemas data was not initialized")
	}
	return i.graphQLSchemas
}

func (i *Instances) Store(ctx context.Context, catalog string, seq iter.Seq2[*declcfg.Meta, error]) error {
	activeInstances := i.activeInstances()
	numActiveInstances := len(activeInstances)

	// copy the sequence `len(i.instances)` times.
	copiedSeqs, cancel := copySequence(seq, numActiveInstances)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(numActiveInstances)

	errs := make([]error, 0, numActiveInstances)
	var errMu sync.Mutex
	for idx := range numActiveInstances {
		// We need to run the instance store functions concurrently because
		// the iterators in copiedSeqs need to be consumed concurrently.
		go func() {
			defer wg.Done()
			if err := activeInstances[idx].Store(ctx, catalog, copiedSeqs[idx]); err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (i *Instances) Delete(ctx context.Context, catalog string) error {
	activeInstances := i.activeInstances()
	errs := make([]error, 0, len(activeInstances))
	for _, instance := range activeInstances {
		errs = append(errs, instance.Delete(ctx, catalog))
	}
	return errors.Join(errs...)
}

func (i *Instances) Exists(catalog string) bool {
	activeInstances := i.activeInstances()
	for _, instance := range activeInstances {
		if !instance.Exists(catalog) {
			return false
		}
	}
	return true
}

func (i *Instances) activeInstances() []Instance {
	instances := []Instance{}
	if i.files != nil {
		instances = append(instances, i.files)
	}
	if i.indices != nil {
		instances = append(instances, i.indices)
	}
	if i.graphQLSchemas != nil {
		instances = append(instances, i.graphQLSchemas)
	}
	return instances
}

// copySequence copies values from the input iterator to n output iterators. Note that this function
// consumes the input iterator, so callers should not use the input iterator after copying it.
//
// Note: Iterators produced by this function must be consumed concurrently. However, consumers can
// independently decide to stop iterating without affecting other consumers.
func copySequence[V any, E any](in iter.Seq2[V, E], n int) ([]iter.Seq2[V, E], context.CancelFunc) {
	if n <= 0 {
		return []iter.Seq2[V, E]{}, func() {}
	}
	if n == 1 {
		return []iter.Seq2[V, E]{in}, func() {}
	}

	type dataVal struct {
		v V
		e E
	}
	type outputPipe struct {
		dataCh chan dataVal
		doneCh chan struct{}
	}

	activePipes := make(map[int]*outputPipe, n)
	outSeqs := make([]iter.Seq2[V, E], n)
	for i := range n {
		pipe := &outputPipe{
			// Buffered data channel of size 1 lets us fan out each input value to all consumers concurrently.
			dataCh: make(chan dataVal, 1),
			doneCh: make(chan struct{}),
		}
		activePipes[i] = pipe

		outSeqs[i] = func(yield func(V, E) bool) {
			// This is the only place that we close the done channel.
			// The fan-out goroutine will be responsible for always closing dataCh,
			// which will ensure that _this_ function returns and doneCh is closed.
			defer func() { close(pipe.doneCh) }()

			// There are two ways for this loop to end:
			//  1. The input iterator is exhausted, and the fan-out goroutine closes dataCh
			//     to signal that there is no more data on the iterator.
			//  2. The consumer of the iterator decides to stop iterating early by returning false
			//     when we call `yield`, so we return.
			for dv := range pipe.dataCh {
				if !yield(dv.v, dv.e) {
					return
				}
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for v, e := range in {
			// Iterate the pipes in index order for determinism.
			for _, i := range slices.Sorted(maps.Keys(activePipes)) {
				pipe := activePipes[i]
				select {
				case pipe.dataCh <- dataVal{v: v, e: e}:

				// If a pipe's doneCh is closed before we've exhausted the input, that means
				// the consumer has stopped consuming from its iterator.
				//
				// If the context is cancelled, that means we need to clean up to avoid
				// leaking channels and goroutines.
				//
				// In either case, we close the pipe's dataCh and remove it from our set of active pipes.
				case <-pipe.doneCh:
					close(pipe.dataCh)
					delete(activePipes, i)
				case <-ctx.Done():
					close(pipe.dataCh)
					delete(activePipes, i)
				}
			}

			// If all pipes have stopped iterating early, we can also stop iterating through the input
			// iterator's values.
			if len(activePipes) == 0 {
				break
			}
		}

		// If there are still activePipes, that means we have exhausted the input iterator, so
		// we close the active pipes' dataCh to signal that there are no more values to consume.
		for _, pipe := range activePipes {
			close(pipe.dataCh)
		}
	}()
	return outSeqs, cancel
}
