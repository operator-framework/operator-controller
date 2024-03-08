package webhook

import (
	"context"

	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type KAppUserInfo struct {
	WhitelistedUsernames []string
}

//+kubebuilder:webhook:path=/kapp-user-info,mutating=false,failurePolicy=fail,sideEffects=None,groups=internal.packaging.carvel.dev;kappctrl.k14s.io;packaging.carvel.dev,resources=*/*,verbs=create;update;delete,versions=*,name=vkappcrsuserinfo.kb.io,admissionReviewVersions=v1

func (k *KAppUserInfo) Handle(ctx context.Context, request admission.Request) admission.Response {
	l := log.FromContext(ctx).WithName("kapp-user-info")

	for idx := range k.WhitelistedUsernames {
		namespace, name, err := serviceaccount.SplitUsername(k.WhitelistedUsernames[idx])
		if err != nil {
			l.Error(err, "unable to parse whitelisted username")
			return admission.Denied("internal error")
		}

		if serviceaccount.MatchesUsername(namespace, name, request.UserInfo.Username) {
			return admission.Allowed("")
		}
	}

	return admission.Denied("this is an internal API. Please use OLM APIs instead")
}

func (k *KAppUserInfo) SetupWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register("/kapp-user-info", (&admission.Webhook{
		Handler:      k,
		RecoverPanic: true,
	}))
	return nil
}

var _ admission.Handler = &KAppUserInfo{}
