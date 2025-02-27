package authentication

import (
	"fmt"

	"k8s.io/client-go/transport"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func SyntheticUserName(ext ocv1.ClusterExtension) string {
	return fmt.Sprintf("olm:clusterextensions:%s", ext.Name)
}

func SyntheticGroups(_ ocv1.ClusterExtension) []string {
	return []string{
		"olm:clusterextensions",
		"system:authenticated",
	}
}

func SyntheticImpersonationConfig(ext ocv1.ClusterExtension) transport.ImpersonationConfig {
	return transport.ImpersonationConfig{
		UserName: SyntheticUserName(ext),
		Groups:   SyntheticGroups(ext),
	}
}
