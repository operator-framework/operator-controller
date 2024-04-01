package deployer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"carvel.dev/imgpkg/pkg/imgpkg/registry"
	imgpkgv1 "carvel.dev/imgpkg/pkg/imgpkg/v1"
	"github.com/cppforlife/go-cli-ui/ui"
	"github.com/vmware-tanzu/carvel-kapp/pkg/kapp/cmd/app"
	"github.com/vmware-tanzu/carvel-kapp/pkg/kapp/cmd/core"
	"github.com/vmware-tanzu/carvel-kapp/pkg/kapp/logger"
	"github.com/vmware-tanzu/carvel-kapp/pkg/kapp/preflight"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type Deployer struct{}

func (d *Deployer) DeployImage(imgRef string) error {
	// Pull the image reference using carvel-dev/imgpkg apis. For quick PoC
	// purposes we are doing this as part of this DeployImage function
	// but in a real implementation we should have this as its own distinct
	// step
	pullOpts := imgpkgv1.PullOpts{
		AsImage:  true,
		IsBundle: false,
		Logger:   &nullLogger{},
	}

	reg, err := registry.NewSimpleRegistry(registry.Opts{Anon: true})
	if err != nil {
		return err
	}

	outDir, err := os.MkdirTemp("", "deployer-")
	if err != nil {
		return err
	}
	outPath := filepath.Join(outDir, imgRef)
	defer os.Remove(outPath)

	_, err = imgpkgv1.PullWithRegistry(imgRef, outPath, pullOpts, reg)
	if err != nil {
		return err
	}

	// Use kapp libraries to deploy the image contents. For now, essentially rebuild the kapp deploy CLI
	// command. A *real* implementation should do more investigation as to what things we do/don't need
	// from the CLI and use the proper lower level packages that the CLI uses to run the CLI operations.
	// For this PoC just assume we are following the "plain+v0" format that we introduced with rukpak so
	// we don't have to handle any registry+v1 conversion logic.
	preflightReg := &preflight.Registry{}
	depFactory := &simpleDepsFactory{
		cfg: config.GetConfigOrDie(),
	}
	fUI := &fakeUI{ui.NewNoopUI()}

	depOpts := app.NewDeployOptions(fUI, depFactory, logger.NewNoopLogger(), preflightReg)
	depOpts.AppFlags.Name = fmt.Sprintf("deployer-%s", strings.ReplaceAll(strings.ReplaceAll(imgRef, "/", "-"), ":", "-"))
	depOpts.AppFlags.NamespaceFlags.Name = "default"
	depOpts.FileFlags.Files = []string{outPath}
	depOpts.ApplyFlags.ApplyingChangesOpts.Concurrency = 1
	depOpts.ApplyFlags.WaitingChangesOpts.Concurrency = 1

	return depOpts.Run()
}

type fakeUI struct {
	ui.UI
}

func (u *fakeUI) AskForConfirmation() error {
	return nil
}

type nullLogger struct{}

var _ imgpkgv1.Logger = (*nullLogger)(nil)

func (l *nullLogger) Errorf(msg string, args ...interface{}) { return }
func (l *nullLogger) Warnf(msg string, args ...interface{})  { return }
func (l *nullLogger) Debugf(msg string, args ...interface{}) { return }
func (l *nullLogger) Tracef(msg string, args ...interface{}) { return }
func (l *nullLogger) Logf(msg string, args ...interface{})   { return }

type simpleDepsFactory struct {
	cfg *rest.Config
}

func (df *simpleDepsFactory) DynamicClient(opts core.DynamicClientOpts) (dynamic.Interface, error) {
	return dynamic.NewForConfig(df.cfg)
}

func (df *simpleDepsFactory) CoreClient() (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(df.cfg)
}

func (df *simpleDepsFactory) RESTMapper() (meta.RESTMapper, error) {
	disc, err := discovery.NewDiscoveryClientForConfig(df.cfg)
	if err != nil {
		return nil, err
	}

	cachedDisc := memory.NewMemCacheClient(disc)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDisc)
	return mapper, nil
}

func (df *simpleDepsFactory) ConfigureWarnings(warnings bool) { return }
