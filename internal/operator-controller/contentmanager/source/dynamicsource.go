package source

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic/dynamicinformer"
	cgocache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	source "github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager/source/internal"
)

type DynamicSourceConfig struct {
	// DynamicInformerFactory is the dynamicinformer.DynamicSharedInformerFactory
	// that is used to generate the informers configured on this sources startup.
	// If you use a dynamicinformer.DynamicSharedInformerFactory that you've
	// used previously, it must not have been used to start a new informer for
	// the same GVR. You can not start or configure an informer that has already
	// been started, even after it has been stopped. Reusing an informer factory
	// may result in attempting to configure and start an informer that has
	// already been started.
	DynamicInformerFactory dynamicinformer.DynamicSharedInformerFactory
	// GVR is the GroupVersionResource that this source is responsible
	// for creating and configuring an informer for
	GVR schema.GroupVersionResource
	// Owner is the client.Object that owns the managed content that this
	// source will be creating an informer to react to events for. This
	// field is used to attempt to requeue the owning client.Object for
	// reconciliation on a watch error after a previously successful sync
	Owner client.Object
	// Handler is the handler.EventHandler that is used to react to events
	// received by the configured source
	Handler handler.EventHandler
	// Predicates are the predicate.Predicate functions used to determine
	// if a triggered event should be reacted to
	Predicates []predicate.Predicate
	// OnPostSyncError is the callback function that is used when the source
	// initially synced successfully and later encountered an error
	OnPostSyncError func(context.Context)
}

func NewDynamicSource(cfg DynamicSourceConfig) *dynamicInformerSource {
	return &dynamicInformerSource{
		cfg:         cfg,
		erroredChan: make(chan struct{}),
		syncedChan:  make(chan struct{}),
		startedChan: make(chan struct{}),
	}
}

// dynamicInformerSource is an implementation of the
// ReaderCloserSyncingSource interface. It is used
// to create an informer, using the provided dynamic informer
// factory, for the configured GroupVersionResource.
//
// The informer is configured with a WatchEventErrorHandler that
// stops the informer, and if it had previously synced successfully
// it attempts to requeue the provided Owner for reconciliation and
// calls the provided OnWatchError function.
type dynamicInformerSource struct {
	cfg            DynamicSourceConfig
	informerCancel context.CancelFunc
	informerCtx    context.Context
	startedChan    chan struct{}
	syncedChan     chan struct{}
	erroredChan    chan struct{}
	errOnce        sync.Once
	err            error
}

func (dis *dynamicInformerSource) String() string {
	return fmt.Sprintf("contentmanager.DynamicInformerSource - GVR: %s, Owner Type: %T, Owner Name: %s", dis.cfg.GVR, dis.cfg.Owner, dis.cfg.Owner.GetName())
}

func (dis *dynamicInformerSource) Start(ctx context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	// Close the startedChan to signal that this
	// source has been started. Subsequent calls
	// to Start will attempt to close a closed channel
	// and panic.
	close(dis.startedChan)

	dis.informerCtx, dis.informerCancel = context.WithCancel(ctx)
	gInf := dis.cfg.DynamicInformerFactory.ForResource(dis.cfg.GVR)
	eventHandler := source.NewEventHandler(dis.informerCtx, q, dis.cfg.Handler, dis.cfg.Predicates)

	// If we encounter an error during the watch we will:
	// - Capture the error
	// - cancel the informer
	// requeuing of the ClusterExtension should happen by the
	// WaitForSync function returning an error
	// Only if we have successfully synced in the past should we
	// requeue the ClusterExtension
	sharedIndexInf := gInf.Informer()
	err := sharedIndexInf.SetWatchErrorHandler(func(r *cgocache.Reflector, err error) {
		dis.errOnce.Do(func() {
			dis.err = err
			close(dis.erroredChan)
		})

		if dis.hasSynced() {
			// We won't be able to update the ClusterExtension status
			// conditions so instead force a requeue if we
			// have previously synced and then errored
			defer q.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: dis.cfg.Owner.GetName(),
				},
			})
			dis.cfg.OnPostSyncError(dis.informerCtx)
		}
		dis.informerCancel()
		cgocache.DefaultWatchErrorHandler(dis.informerCtx, r, err)
	})
	if err != nil {
		return fmt.Errorf("setting WatchErrorHandler: %w", err)
	}

	_, err = sharedIndexInf.AddEventHandler(eventHandler.HandlerFuncs())
	if err != nil {
		return fmt.Errorf("adding event handler: %w", err)
	}

	go sharedIndexInf.Run(dis.informerCtx.Done())

	go func() {
		syncOnce := sync.OnceFunc(func() {
			dis.syncedChan <- struct{}{}
			close(dis.syncedChan)
		})

		_ = wait.PollUntilContextCancel(dis.informerCtx, time.Millisecond*100, true, func(_ context.Context) (bool, error) {
			if sharedIndexInf.HasSynced() {
				syncOnce()
				return true, nil
			}
			return false, nil
		})
	}()

	return nil
}

func (dis *dynamicInformerSource) WaitForSync(ctx context.Context) error {
	if !dis.hasStarted() {
		return fmt.Errorf("not yet started")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-dis.erroredChan:
		return dis.err
	case <-dis.informerCtx.Done():
		return dis.informerCtx.Err()
	case <-dis.syncedChan:
		return nil
	}
}

func (dis *dynamicInformerSource) Close() error {
	if !dis.hasStarted() {
		return errors.New("source has not yet started")
	}
	dis.informerCancel()
	return nil
}

func (dis *dynamicInformerSource) hasSynced() bool {
	select {
	case <-dis.syncedChan:
		return true
	default:
		return false
	}
}

func (dis *dynamicInformerSource) hasStarted() bool {
	select {
	case <-dis.startedChan:
		return true
	default:
		return false
	}
}
