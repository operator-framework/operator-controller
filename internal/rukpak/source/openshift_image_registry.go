package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/pkg/sysregistriesv2"
	"github.com/containers/image/v5/signature"
	imgstorage "github.com/containers/image/v5/storage"
	"github.com/containers/image/v5/types"
	"github.com/containers/storage"
	"github.com/opencontainers/go-digest"
	ocpconfigv1 "github.com/openshift/api/config/v1"
	ocpoperatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/runtime-utils/pkg/registries"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type OpenShiftImageRegistry struct {
	Client                   client.Client
	RegistriesConfigFilePath string
	StorageRootPath          string
}

func (i *OpenShiftImageRegistry) AddToScheme(s *runtime.Scheme) error {
	if err := ocpconfigv1.AddToScheme(s); err != nil {
		return err
	}
	if err := ocpoperatorv1alpha1.AddToScheme(s); err != nil {
		return err
	}
	return nil
}

func (i *OpenShiftImageRegistry) SetupWithManager(mgr ctrl.Manager) error {
	i.Client = mgr.GetClient()

	// Create the controller
	ctr, err := controller.New("openshift-image-registry", mgr, controller.Options{
		MaxConcurrentReconciles: 1,
		Reconciler:              i,
	})
	if err != nil {
		return err
	}

	// Establish the watches
	if err := ctr.Watch(source.Kind[*ocpconfigv1.ImageTagMirrorSet](
		mgr.GetCache(),
		&ocpconfigv1.ImageTagMirrorSet{},
		handler.TypedEnqueueRequestsFromMapFunc[*ocpconfigv1.ImageTagMirrorSet](func(ctx context.Context, obj *ocpconfigv1.ImageTagMirrorSet) []ctrl.Request {
			return []ctrl.Request{{NamespacedName: client.ObjectKey{Name: "openshift-image-registry-config"}}}
		}),
	)); err != nil {
		return err
	}
	if err := ctr.Watch(source.Kind[*ocpconfigv1.ImageDigestMirrorSet](
		mgr.GetCache(),
		&ocpconfigv1.ImageDigestMirrorSet{},
		handler.TypedEnqueueRequestsFromMapFunc[*ocpconfigv1.ImageDigestMirrorSet](func(ctx context.Context, obj *ocpconfigv1.ImageDigestMirrorSet) []ctrl.Request {
			return []ctrl.Request{{NamespacedName: client.ObjectKey{Name: "openshift-image-registry-config"}}}
		}),
	)); err != nil {
		return err
	}
	if err := ctr.Watch(source.Kind[*ocpoperatorv1alpha1.ImageContentSourcePolicy](
		mgr.GetCache(),
		&ocpoperatorv1alpha1.ImageContentSourcePolicy{},
		handler.TypedEnqueueRequestsFromMapFunc[*ocpoperatorv1alpha1.ImageContentSourcePolicy](func(ctx context.Context, obj *ocpoperatorv1alpha1.ImageContentSourcePolicy) []ctrl.Request {
			return []ctrl.Request{{NamespacedName: client.ObjectKey{Name: "openshift-image-registry-config"}}}
		}),
	)); err != nil {
		return err
	}
	return nil
}

func (i *OpenShiftImageRegistry) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	// We don't actually care about the request, we just want to reconcile the entire registry config
	var (
		itmsList ocpconfigv1.ImageTagMirrorSetList
		idmsList ocpconfigv1.ImageDigestMirrorSetList
		icspList ocpoperatorv1alpha1.ImageContentSourcePolicyList
	)
	var wg sync.WaitGroup
	wg.Add(3)
	errChan := make(chan error)
	go func() {
		defer wg.Done()
		if err := i.Client.List(ctx, &itmsList); err != nil {
			errChan <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := i.Client.List(ctx, &idmsList); err != nil {
			errChan <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := i.Client.List(ctx, &icspList); err != nil {
			errChan <- err
		}
	}()
	go func() {
		wg.Wait()
		close(errChan)
	}()

	var listErrs []error
	for err := range errChan {
		listErrs = append(listErrs, err)
	}
	if len(listErrs) > 0 {
		return ctrl.Result{}, errors.Join(listErrs...)
	}

	registriesConf := sysregistriesv2.V2RegistriesConf{}
	if err := registries.EditRegistriesConfig(&registriesConf, nil, nil,
		toPtrList(icspList.Items),
		toPtrList(idmsList.Items),
		toPtrList(itmsList.Items),
	); err != nil {
		return ctrl.Result{}, err
	}

	f, err := os.Create(i.RegistriesConfigFilePath)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := toml.NewEncoder(f).Encode(registriesConf); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func toPtrList[T any](l []T) []*T {
	ptrs := make([]*T, 0, len(l))
	for i := range l {
		ptrs = append(ptrs, &l[i])
	}
	return ptrs
}

func (i *OpenShiftImageRegistry) Unpack(ctx context.Context, bundle *BundleSource) (*Result, error) {
	l := log.FromContext(ctx)

	l.Info("1")
	if bundle.Type != SourceTypeImage {
		panic(fmt.Sprintf("programmer error: source type %q is unable to handle specified bundle source type %q", SourceTypeImage, bundle.Type))
	}

	l.Info("2")
	if bundle.Image == nil {
		return nil, NewUnrecoverable(fmt.Errorf("error parsing bundle, bundle %s has a nil image source", bundle.Name))
	}

	l.Info("3")
	imgRef, err := reference.ParseNamed(bundle.Image.Ref)
	if err != nil {
		return nil, NewUnrecoverable(fmt.Errorf("error parsing image reference: %w", err))
	}

	l.Info("4")
	srcCtx := &types.SystemContext{}
	if s, err := os.Stat(i.RegistriesConfigFilePath); err == nil && !s.IsDir() {
		srcCtx.SystemRegistriesConfPath = i.RegistriesConfigFilePath
	}
	l.Info("5")
	srcRef, err := docker.NewReference(imgRef)
	if err != nil {
		return nil, NewUnrecoverable(fmt.Errorf("error creating reference: %w", err))
	}
	l.Info("6")

	store, err := i.newStore(bundle.Name)
	if err != nil {
		return nil, fmt.Errorf("error creating store: %w", err)
	}

	storeTport := imgstorage.Transport
	destRef, err := storeTport.ParseReference(imgRef.String())
	if err != nil {
		return nil, fmt.Errorf("error creating reference: %w", err)
	}

	policy, err := signature.NewPolicyFromBytes([]byte(`{"default":[{"type":"insecureAcceptAnything"}]}`))
	if err != nil {
		return nil, NewUnrecoverable(fmt.Errorf("error getting default policy: %w", err))
	}
	policyContext, err := signature.NewPolicyContext(policy)
	if err != nil {
		return nil, NewUnrecoverable(fmt.Errorf("error getting policy context: %w", err))
	}
	defer policyContext.Destroy()

	destCtx := &types.SystemContext{
		ArchitectureChoice: "amd64",
		OSChoice:           "linux",
	}
	l.Info("8", "ref", imgRef.String())
	manifestData, err := copy.Image(ctx, policyContext, destRef, srcRef, &copy.Options{
		SourceCtx:       srcCtx,
		DestinationCtx:  destCtx,
		PreserveDigests: true,
	})
	l.Info("9", "manifestData", manifestData, "err", err)
	if err != nil {
		return nil, fmt.Errorf("error copying image: %w", err)
	}

	l.Info("pulled image", "ref", imgRef.String())

	dgst, err := manifest.Digest(manifestData)
	if err != nil {
		return nil, fmt.Errorf("error getting digest of image: %w", err)
	}

	resolvedRef, err := reference.WithDigest(imgRef, dgst)
	if err != nil {
		return nil, fmt.Errorf("error creating resolved reference: %w", err)
	}

	imgs, err := store.ImagesByDigest(dgst)
	if err != nil {
		return nil, fmt.Errorf("error getting images by digest: %w", err)
	}

	var id string
	for _, img := range imgs {
		if sets.New[string](img.Names...).Has(bundle.Image.Ref) {
			id = img.ID
			break
		}
	}
	if id == "" {
		return nil, fmt.Errorf("image not found in store")
	}

	mountPoint, err := store.MountImage(id, nil, bundle.Name)
	if err != nil {
		return nil, fmt.Errorf("error mounting image: %w", err)
	}

	if err := i.deleteOtherImages(ctx, storeTport, dgst); err != nil {
		return nil, fmt.Errorf("error deleting old images: %w", err)
	}

	return &Result{
		Bundle:         os.DirFS(mountPoint),
		ResolvedSource: &BundleSource{Type: SourceTypeImage, Name: bundle.Name, Image: &ImageSource{Ref: resolvedRef.String()}},
		State:          StateUnpacked,
		Message:        "unpacked successfully",
	}, nil
}

func (i *OpenShiftImageRegistry) deleteOtherImages(ctx context.Context, storeTransport imgstorage.StoreTransport, digestToKeep digest.Digest) error {
	l := log.FromContext(ctx)
	// Read the oci layout index
	store := storeTransport.GetStoreIfSet()
	imgs, err := store.Images()
	if err != nil {
		return err
	}

	var errs []error
	for _, img := range imgs {
		if img.Digest == digestToKeep {
			continue
		}
		if _, err := store.UnmountImage(img.ID, true); err != nil {
			errs = append(errs, err)
			continue
		}
		l.Info("unmounted image", "digest", img.Digest.String())

		_, err := store.DeleteImage(img.ID, true)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		l.Info("deleted image", "digest", img.Digest.String())
	}
	return errors.Join(errs...)
}

func (i *OpenShiftImageRegistry) newStore(name string) (storage.Store, error) {
	wd := filepath.Join(i.StorageRootPath, name)
	if err := os.MkdirAll(wd, 0755); err != nil {
		return nil, err
	}
	run := filepath.Join(wd, "run")
	root := filepath.Join(wd, "root")
	//imgstorage.Transport.SetDefaultUIDMap([]idtools.IDMap{{
	//	ContainerID: 0,
	//	HostID:      os.Getuid(),
	//	Size:        1,
	//}})
	//imgstorage.Transport.SetDefaultGIDMap([]idtools.IDMap{{
	//	ContainerID: 0,
	//	HostID:      os.Getgid(),
	//	Size:        1,
	//}})
	store, err := storage.GetStore(storage.StoreOptions{
		RunRoot:            run,
		GraphRoot:          root,
		GraphDriverName:    "vfs",
		GraphDriverOptions: []string{},
	})
	if err != nil {
		return nil, err
	}
	imgstorage.Transport.SetStore(store)
	return store, nil
}
func (i *OpenShiftImageRegistry) Cleanup(_ context.Context, bundle *BundleSource) error {
	return os.RemoveAll(filepath.Join(i.StorageRootPath, bundle.Name))
}
