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
	Files          *Files
	Indices        *Indices
	GraphQLSchemas *GraphQLSchemas
}

func (i *Instances) Store(ctx context.Context, catalog string, seq iter.Seq2[*declcfg.Meta, error]) error {
	activeInstances := i.activeInstances()
	numInstances := len(activeInstances)

	// copy the sequence `len(i.instances)` times.
	copiedSeqs := copySequence(seq, numInstances)

	var wg sync.WaitGroup
	wg.Add(numInstances)

	errs := make([]error, 0, numInstances)
	for idx := range numInstances {
		go func() {
			defer wg.Done()
			errs = append(errs, activeInstances[idx].Store(ctx, catalog, copiedSeqs[idx]))
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
	for _, instance := range []Instance{i.Files, i.Indices, i.GraphQLSchemas} {
		if instance != nil {
			instances = append(instances, instance)
		}
	}
	return instances
}

// copySequence copies values from the input iterator to n output iterators. Note that this function
// consumes the input iterator, so callers should not use the input iterator after copying it.
//
// Note: Files output iterators produced by this function must be consumed concurrently. However, consumers can
// independently decide to stop iterating without affecting other consumers.
func copySequence[V any, E any](in iter.Seq2[V, E], n int) []iter.Seq2[V, E] {
	if n <= 0 {
		return []iter.Seq2[V, E]{}
	}
	if n == 1 {
		return []iter.Seq2[V, E]{in}
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

	go func() {
		for v, e := range in {
			// Iterate the pipes in index order for determinism.
			for _, i := range slices.Sorted(maps.Keys(activePipes)) {
				pipe := activePipes[i]
				select {
				case pipe.dataCh <- dataVal{v: v, e: e}:
				case <-pipe.doneCh:
					// If a pipe's doneCh is closed before we've exhausted the input, that means
					// the consumer has stopped consuming from its iterator. We can close its
					// dataCh and remove it from our set of active pipes.
					close(pipe.dataCh)
					delete(activePipes, i)
					continue
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
	return outSeqs
}
