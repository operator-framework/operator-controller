package unit

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func SetupClient() (client.Client, error) {
	testenv := &envtest.Environment{}

	config, err := testenv.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start envtest: %v", err)
	}

	kubeclient, err := client.New(config, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubeclient: %v", err)
	}

	return kubeclient, nil
}
