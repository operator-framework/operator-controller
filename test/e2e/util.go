package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	pollInterval = 1 * time.Second
	pollDuration = 5 * time.Minute
)

// HandleTestCaseFailure is responsible for attempting to collect relevant
// testing artifacts when individual test cases fail. In the case that
// a test passes, then this function is a no-op and will return a nil error.
func HandleTestCaseFailure() error {
	currentTest := ginkgo.CurrentSpecReport()
	if !currentTest.Failed() {
		return nil
	}

	// current test case failed. attempt to collect CI artifacts if the
	// $ARTIFACT_DIR environment variable has been set.
	artifactDir := os.Getenv("ARTIFACT_DIR")
	if artifactDir == "" {
		ginkgo.GinkgoT().Logf("not gathering testing artifacts as $ARTIFACT_DIR is unset")
		return nil
	}
	// check whether the $KUBECONFIG environment variable is empty to
	// determine whether we should fall back to the default path.
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		// Expand $HOME variable as the exec package intentionally does
		// not expand this value when invoking a shell.
		kubeconfig = os.ExpandEnv("$HOME/.kube/config")
	}

	// create a dedicated test case directory to avoid overwriting the
	// testing artifacts gathered by a previous test case failure.
	testCaseDir := filepath.Join(artifactDir, strings.ReplaceAll(currentTest.LeafNodeText, " ", "-"))
	if err := os.MkdirAll(testCaseDir, os.ModePerm); err != nil {
		return err
	}

	cmd := exec.Command("/bin/bash", "-c", "./collect-ci-artifacts.sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	envVars := []string{
		"ARTIFACT_DIR=" + testCaseDir,
		"KUBECONFIG=" + kubeconfig,
		"KUBECTL=" + os.Getenv("KUBECTL"),
	}
	cmd.Env = append(os.Environ(), envVars...)

	return cmd.Run()
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

// waitFor wraps wait.Pool with default polling parameters
func waitFor(fn func() (bool, error)) error {
	return wait.Poll(pollInterval, pollDuration, func() (bool, error) {
		return fn()
	})
}
