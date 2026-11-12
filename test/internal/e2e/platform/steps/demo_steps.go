package steps

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/cucumber/godog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func RegisterDemoSteps(sc *godog.ScenarioContext) {
	sc.Step(`^(?i)catalog "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+)$`, CatalogReportsConditionWithoutReason)
	sc.Step(`^(?i)catalog "([^"]+)" contains some packages$`, CatalogContainsSomePackages)
	sc.Step(`^(?i)package "([^"]+)" in catalog "([^"]+)" has some channels defined$`, PackageHasSomeChannels)
	sc.Step(`^(?i)package "([^"]+)" in catalog "([^"]+)" has some bundles published$`, PackageHasSomeBundles)
	sc.Step(`^(?i)rolebindings in namespace "([^"]+)" reference service account "([^"]+)" in namespace "([^"]+)"$`, RolebindingsReferenceServiceAccount)
	sc.Step(`^(?i)pod "([^"]+)" in test namespace has (\d+) containers$`, PodHasContainerCount)
}

func bash(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	shared.Log().V(1).Info("Running", "command", script)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	if err != nil {
		shared.Log().V(1).Info("Failed to run", "command", script, "stderr", stderr, "error", err)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitErr.Stderr = stderrBuf.Bytes()
		}
	}
	shared.Log().V(1).Info("Output", "command", script, "output", stdout)

	if rec := shared.CommandRecorderFromContext(ctx); rec != nil {
		rec.RecordCommand(script, stdout, stderr, elapsed)
	}
	return stdout, err
}

func CatalogReportsConditionWithoutReason(ctx context.Context, catalogUserName, conditionType, conditionStatus string) error {
	sc := shared.Scenario(ctx)
	catalogName, ok := sc.Catalogs[catalogUserName]
	if !ok {
		if _, err := shared.K8sClient(ctx, "get", "clustercatalog", catalogUserName); err != nil {
			return fmt.Errorf("catalog %q was not created by this scenario and does not exist on the cluster", catalogUserName)
		}
		catalogName = catalogUserName
	}
	err := shared.WaitForCondition(ctx, "clustercatalog", catalogName, conditionType, metav1.ConditionStatus(conditionStatus), nil, nil)
	if err == nil {
		if rec := shared.CommandRecorderFromContext(ctx); rec != nil {
			out, _ := shared.K8sClient(ctx, "get", "clustercatalog", catalogName,
				"-o", fmt.Sprintf("jsonpath={.status.conditions[?(@.type==\"%s\")]}", conditionType))
			var pretty bytes.Buffer
			if jsonErr := json.Indent(&pretty, []byte(out), "", "  "); jsonErr == nil {
				out = pretty.String()
			}
			rec.RecordCustom(
				fmt.Sprintf("kubectl get clustercatalog %s -o jsonpath='{.status.conditions[?(@.type==\"%s\")]}' | jq .",
					catalogName, conditionType),
				out+"\n", "",
			)
		}
	}
	return err
}

func ensureCatalogPortForward(ctx context.Context) (string, error) {
	sc := shared.Scenario(ctx)
	if sc.CatalogAddr != "" {
		if catalogPortForwardAlive(sc.CatalogAddr) {
			return sc.CatalogAddr, nil
		}
		shared.Log().V(1).Info("Catalog port-forward is dead, re-establishing", "addr", sc.CatalogAddr)
		resetCatalogPortForward(ctx)
	}

	ns := shared.ComponentNamespaces[shared.CatalogdComponent]
	target, err := catalogdLeaderPod(ctx, ns)
	port := int32(443)
	if err != nil {
		shared.Log().V(1).Info("Could not resolve catalogd leader pod, falling back to service", "error", err)
		target = "service/catalogd-service"
	} else {
		port = 8443
	}

	addr, cleanup, err := shared.PortForward(ctx, ns, target, port)
	if err != nil {
		return "", fmt.Errorf("failed to start catalog port-forward to %s: %w", target, err)
	}
	sc.CatalogAddr = addr
	sc.CatalogCleanup = cleanup

	shared.WaitFor(ctx, func() bool {
		return catalogPortForwardAlive(addr)
	})
	return addr, nil
}

func catalogdLeaderPod(ctx context.Context, ns string) (string, error) {
	holder, err := shared.K8sClient(ctx, "get", "lease", "catalogd-operator-lock", "-n", ns,
		"-o", "jsonpath={.spec.holderIdentity}")
	if err != nil {
		return "", fmt.Errorf("failed to get catalogd leader lease: %w", err)
	}
	holder = strings.TrimSpace(holder)
	podName := holder
	if idx := strings.LastIndex(holder, "_"); idx >= 0 {
		podName = holder[:idx]
	}
	if podName == "" {
		return "", fmt.Errorf("catalogd leader lease has empty holderIdentity")
	}
	shared.Log().Info("Resolved catalogd leader pod", "holder", holder, "pod", podName)
	return fmt.Sprintf("pod/%s", podName), nil
}

func catalogPortForwardAlive(addr string) bool {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			DialContext:     (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		},
	}
	resp, err := client.Get(fmt.Sprintf("https://%s/", addr))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// resetCatalogPortForward tears down the cached port-forward so the next
// call to ensureCatalogPortForward establishes a fresh connection.  With
// CatalogdHA, non-leader pods return 404 (empty local cache); resetting
// lets the next retry potentially reach the leader pod.
func resetCatalogPortForward(ctx context.Context) {
	sc := shared.Scenario(ctx)
	if sc.CatalogCleanup != nil {
		sc.CatalogCleanup()
	}
	sc.CatalogAddr = ""
	sc.CatalogCleanup = nil
}

func catalogCurlJq(ctx context.Context, catalogName, jqFilter string) (string, error) {
	addr, err := ensureCatalogPortForward(ctx)
	if err != nil {
		return "", err
	}
	// pipefail: propagate curl exit code through the pipe (e.g. HTTP 404 from a non-leader catalogd pod).
	// -sS: silent but show errors on stderr.  -k: skip TLS verification for the port-forward.
	// --compressed: request gzip and stream-decompress (catalogd uses gzhttp); saves network for the large operatorhubio catalog.
	// --fail: exit 22 on HTTP errors so non-JSON error bodies don't reach jq.
	script := fmt.Sprintf(
		`set -o pipefail; curl -sS -k --compressed --fail https://%s/catalogs/%s/api/v1/all | jq '%s'`,
		addr, catalogName, jqFilter,
	)
	out, err := bash(ctx, script)
	if err != nil {
		resetCatalogPortForward(ctx)
	}
	return out, err
}

func CatalogContainsSomePackages(ctx context.Context, catalogName string) error {
	shared.WaitFor(ctx, func() bool {
		out, err := catalogCurlJq(ctx, catalogName,
			`objects | select(.schema == "olm.package") | .name`)
		if err != nil {
			shared.Log().Info("Catalog query failed, retrying", "catalog", catalogName, "error", err, "stderr", shared.StderrOutput(err))
			return false
		}
		if strings.TrimSpace(out) == "" {
			shared.Log().Info("Catalog returned no packages, retrying", "catalog", catalogName)
			return false
		}
		return true
	})
	return nil
}

func PackageHasSomeChannels(ctx context.Context, packageName, catalogName string) error {
	shared.WaitFor(ctx, func() bool {
		out, err := catalogCurlJq(ctx, catalogName,
			fmt.Sprintf(`objects | select(.schema == "olm.channel") | select(.package == "%s") | .name`, packageName))
		if err != nil {
			shared.Log().Info("Catalog query failed, retrying", "catalog", catalogName, "package", packageName, "error", err, "stderr", shared.StderrOutput(err))
			return false
		}
		if strings.TrimSpace(out) == "" {
			shared.Log().Info("Package has no channels, retrying", "catalog", catalogName, "package", packageName)
			return false
		}
		return true
	})
	return nil
}

func PackageHasSomeBundles(ctx context.Context, packageName, catalogName string) error {
	shared.WaitFor(ctx, func() bool {
		out, err := catalogCurlJq(ctx, catalogName,
			fmt.Sprintf(`objects | select(.schema == "olm.bundle") | select(.package == "%s") | .name`, packageName))
		if err != nil {
			shared.Log().Info("Catalog query failed, retrying", "catalog", catalogName, "package", packageName, "error", err, "stderr", shared.StderrOutput(err))
			return false
		}
		if strings.TrimSpace(out) == "" {
			shared.Log().Info("Package has no bundles, retrying", "catalog", catalogName, "package", packageName)
			return false
		}
		return true
	})
	return nil
}

func RolebindingsReferenceServiceAccount(ctx context.Context, rbNamespace, saName, saNamespace string) error {
	saNamespace = shared.SubstituteScenarioVars(saNamespace, shared.Scenario(ctx))
	out, err := shared.K8sClient(ctx, "get", "rolebindings", "-n", rbNamespace, "-o", "json")
	if err != nil {
		return fmt.Errorf("failed to list rolebindings in namespace %q: %w", rbNamespace, err)
	}

	var rbList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Subjects []struct {
				Kind      string `json:"kind"`
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"subjects"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &rbList); err != nil {
		return fmt.Errorf("failed to parse rolebindings: %w", err)
	}

	for _, rb := range rbList.Items {
		for _, s := range rb.Subjects {
			if s.Kind == "ServiceAccount" && s.Name == saName && s.Namespace == saNamespace {
				if rec := shared.CommandRecorderFromContext(ctx); rec != nil {
					subjectsOut, _ := shared.K8sClient(ctx, "get", "rolebinding", rb.Metadata.Name, "-n", rbNamespace,
						"-o", "jsonpath={.subjects}")
					rec.RecordCustom(
						fmt.Sprintf("kubectl get rolebinding %s -n %s -o jsonpath='{.subjects}' | jq .", rb.Metadata.Name, rbNamespace),
						subjectsOut+"\n", "",
					)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("no rolebinding in namespace %q references service account %s/%s", rbNamespace, saNamespace, saName)
}

func PodHasContainerCount(ctx context.Context, podName string, expected int) error {
	sc := shared.Scenario(ctx)
	var actual int
	shared.WaitFor(ctx, func() bool {
		out, err := shared.K8sClient(ctx, "get", "pod", podName, "-n", sc.Namespace, "-o", "jsonpath={.spec.containers[*].name}")
		if err != nil {
			return false
		}
		names := strings.Fields(strings.TrimSpace(out))
		actual = len(names)
		return actual == expected
	})
	if rec := shared.CommandRecorderFromContext(ctx); rec != nil {
		out, _ := shared.K8sClient(ctx, "get", "pod", podName, "-n", sc.Namespace, "-o", "jsonpath={.spec.containers[*].name}")
		rec.RecordCustom(
			fmt.Sprintf("kubectl get pod %s -n %s -o jsonpath='{.spec.containers[*].name}'", podName, sc.Namespace),
			out+"\n", "",
		)
	}
	return nil
}
