package e2e

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	pollInterval = 1 * time.Second
	pollDuration = 60 * time.Second
)

// waitFor wraps wait.Pool with default polling parameters
func waitFor(fn func() (bool, error)) error {
	return wait.Poll(pollInterval, pollDuration, func() (bool, error) {
		return fn()
	})
}

func SetupTestNamespace(c client.Client, name string) *corev1.Namespace {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Eventually(func() error {
		return c.Create(context.Background(), ns)
	}).Should(Succeed())

	return ns
}
