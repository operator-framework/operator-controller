package authentication

import (
	"fmt"

	"k8s.io/client-go/transport"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func syntheticUserName(ext ocv1.ClusterExtension) string {
	return fmt.Sprintf("olm:clusterextension:%s", ext.Name)
}

func syntheticGroups(_ ocv1.ClusterExtension) []string {
	return []string{
		"olm:clusterextensions",
	}
}

func SyntheticImpersonationConfig(ext ocv1.ClusterExtension) transport.ImpersonationConfig {
	return transport.ImpersonationConfig{
		UserName: syntheticUserName(ext),
		Groups:   syntheticGroups(ext),
	}
}
