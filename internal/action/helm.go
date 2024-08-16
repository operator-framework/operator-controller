package action

import (
	"context"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"sigs.k8s.io/controller-runtime/pkg/client"

	actionclient "github.com/operator-framework/operator-controller/internal/helm/client"

	olmv1error "github.com/operator-framework/operator-controller/internal/action/error"
)

type ActionClientGetter struct {
	actionclient.ActionClientGetter
}

func (a ActionClientGetter) ActionClientFor(ctx context.Context, obj client.Object) (actionclient.ActionInterface, error) {
	ac, err := a.ActionClientGetter.ActionClientFor(ctx, obj)
	if err != nil {
		return nil, err
	}
	return &ActionClient{
		ActionInterface:             ac,
		actionClientErrorTranslator: olmv1error.AsOlmErr,
	}, nil
}

func NewWrappedActionClientGetter(acg actionclient.ActionConfigGetter, opts ...actionclient.ActionClientGetterOption) (actionclient.ActionClientGetter, error) {
	ag, err := actionclient.NewActionClientGetter(acg, opts...)
	if err != nil {
		return nil, err
	}
	return &ActionClientGetter{
		ActionClientGetter: ag,
	}, nil
}

type ActionClientErrorTranslator func(err error) error

type ActionClient struct {
	actionclient.ActionInterface
	actionClientErrorTranslator ActionClientErrorTranslator
}

func NewWrappedActionClient(ca actionclient.ActionInterface, errTranslator ActionClientErrorTranslator) actionclient.ActionInterface {
	return &ActionClient{
		ActionInterface:             ca,
		actionClientErrorTranslator: errTranslator,
	}
}

func (a ActionClient) Install(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...actionclient.InstallOption) (*release.Release, error) {
	rel, err := a.ActionInterface.Install(name, namespace, chrt, vals, opts...)
	err = a.actionClientErrorTranslator(err)
	return rel, err
}

func (a ActionClient) Upgrade(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...actionclient.UpgradeOption) (*release.Release, error) {
	rel, err := a.ActionInterface.Upgrade(name, namespace, chrt, vals, opts...)
	err = a.actionClientErrorTranslator(err)
	return rel, err
}

func (a ActionClient) Uninstall(name string, opts ...actionclient.UninstallOption) (*release.UninstallReleaseResponse, error) {
	resp, err := a.ActionInterface.Uninstall(name, opts...)
	err = a.actionClientErrorTranslator(err)
	return resp, err
}

func (a ActionClient) Get(name string, opts ...actionclient.GetOption) (*release.Release, error) {
	resp, err := a.ActionInterface.Get(name, opts...)
	err = a.actionClientErrorTranslator(err)
	return resp, err
}

func (a ActionClient) Reconcile(rel *release.Release) error {
	return a.actionClientErrorTranslator(a.ActionInterface.Reconcile(rel))
}
