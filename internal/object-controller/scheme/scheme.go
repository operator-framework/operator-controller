package scheme

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

var Scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(Scheme))
	// Object management does not require the ClusterExtension or ClusterCatalog APIs.
	Scheme.AddKnownTypes(ocv1.GroupVersion, &ocv1.ClusterObjectSet{}, &ocv1.ClusterObjectSetList{})
	metav1.AddToGroupVersion(Scheme, ocv1.GroupVersion)
}
