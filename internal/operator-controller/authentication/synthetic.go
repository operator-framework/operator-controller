package authentication

import (
	"fmt"

	"k8s.io/client-go/transport"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func SyntheticUserName(ext ocv1.ClusterExtension) string {
	return fmt.Sprintf("olmv1:clusterextensions:%s:admin", ext.Name)
}

func SyntheticGroups(_ ocv1.ClusterExtension) []string {
	return []string{
		"olmv1:clusterextensions:admin",
		"system:authenticated",
	}
}

func SyntheticImpersonationConfig(ext ocv1.ClusterExtension) transport.ImpersonationConfig {
	return transport.ImpersonationConfig{
		UserName: SyntheticUserName(ext),
		Groups:   SyntheticGroups(ext),
	}
}
