package authentication

import (
	"fmt"

	"k8s.io/client-go/transport"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// ServiceAccountImpersonationConfig returns an ImpersonationConfig for impersonating a ServiceAccount.
// This allows authentication as a ServiceAccount without requiring an actual token.
func ServiceAccountImpersonationConfig(ext ocv1.ClusterExtension) transport.ImpersonationConfig {
	return transport.ImpersonationConfig{
		UserName: fmt.Sprintf("system:serviceaccount:%s:%s", ext.Spec.Namespace, ext.Spec.ServiceAccount.Name),
	}
}
