package scheme

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	catalogd "github.com/operator-framework/catalogd/api/v1"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var Scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(ocv1alpha1.AddToScheme(Scheme))
	utilruntime.Must(catalogd.AddToScheme(Scheme))
	utilruntime.Must(appsv1.AddToScheme(Scheme))
	utilruntime.Must(corev1.AddToScheme(Scheme))
	//+kubebuilder:scaffold:scheme
}
