package steps

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/cucumber/godog"
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
	logger.V(1).Info("Running", "command", script)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	if err != nil {
		logger.V(1).Info("Failed to run", "command", script, "stderr", stderr, "error", err)
	}
	logger.V(1).Info("Output", "command", script, "output", stdout)

	if rec := RecorderFromContext(ctx); rec != nil {
		rec.RecordCommand(script, stdout, stderr, elapsed)
	}
	return stdout, err
}

func CatalogReportsConditionWithoutReason(ctx context.Context, catalogUserName, conditionType, conditionStatus string) error {
	sc := scenarioCtx(ctx)
	catalogName, ok := sc.catalogs[catalogUserName]
	if !ok {
		if _, err := k8sClient(ctx, "get", "clustercatalog", catalogUserName); err != nil {
			return fmt.Errorf("catalog %q was not created by this scenario and does not exist on the cluster", catalogUserName)
		}
		catalogName = catalogUserName
	}
	err := waitForCondition(ctx, "clustercatalog", catalogName, conditionType, conditionStatus, nil, nil)
	if err == nil {
		if rec := RecorderFromContext(ctx); rec != nil {
			out, _ := k8sClient(ctx, "get", "clustercatalog", catalogName,
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
	sc := scenarioCtx(ctx)
	if sc.catalogAddr != "" {
		return sc.catalogAddr, nil
	}

	addr, cleanup, err := portForward(ctx, componentNamespaces["catalogd"], "service/catalogd-service", 443)
	if err != nil {
		return "", fmt.Errorf("failed to start catalog port-forward: %w", err)
	}
	sc.catalogAddr = addr
	sc.catalogCleanup = cleanup

	waitFor(ctx, func() bool {
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
	})
	return addr, nil
}

func catalogCurlJq(ctx context.Context, catalogName, jqFilter string) (string, error) {
	addr, err := ensureCatalogPortForward(ctx)
	if err != nil {
		return "", err
	}
	script := fmt.Sprintf(
		`curl -s -k https://%s/catalogs/%s/api/v1/all | jq -s '%s'`,
		addr, catalogName, jqFilter,
	)
	return bash(ctx, script)
}

func CatalogContainsSomePackages(ctx context.Context, catalogName string) error {
	out, err := catalogCurlJq(ctx, catalogName,
		`.[] | select(.schema == "olm.package") | .name`)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("catalog %q contains no packages", catalogName)
	}
	return nil
}

func PackageHasSomeChannels(ctx context.Context, packageName, catalogName string) error {
	out, err := catalogCurlJq(ctx, catalogName,
		fmt.Sprintf(`.[] | select(.schema == "olm.channel") | select(.package == "%s") | .name`, packageName))
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("package %q in catalog %q has no channels", packageName, catalogName)
	}
	return nil
}

func PackageHasSomeBundles(ctx context.Context, packageName, catalogName string) error {
	out, err := catalogCurlJq(ctx, catalogName,
		fmt.Sprintf(`.[] | select(.schema == "olm.bundle") | select(.package == "%s") | .name`, packageName))
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("package %q in catalog %q has no bundles", packageName, catalogName)
	}
	return nil
}

func RolebindingsReferenceServiceAccount(ctx context.Context, rbNamespace, saName, saNamespace string) error {
	saNamespace = substituteScenarioVars(saNamespace, scenarioCtx(ctx))
	out, err := k8sClient(ctx, "get", "rolebindings", "-n", rbNamespace, "-o", "json")
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
				if rec := RecorderFromContext(ctx); rec != nil {
					subjectsOut, _ := k8sClient(ctx, "get", "rolebinding", rb.Metadata.Name, "-n", rbNamespace,
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
	sc := scenarioCtx(ctx)
	var actual int
	waitFor(ctx, func() bool {
		out, err := k8sClient(ctx, "get", "pod", podName, "-n", sc.namespace, "-o", "jsonpath={.spec.containers[*].name}")
		if err != nil {
			return false
		}
		names := strings.Fields(strings.TrimSpace(out))
		actual = len(names)
		return actual == expected
	})
	if rec := RecorderFromContext(ctx); rec != nil {
		out, _ := k8sClient(ctx, "get", "pod", podName, "-n", sc.namespace, "-o", "jsonpath={.spec.containers[*].name}")
		rec.RecordCustom(
			fmt.Sprintf("kubectl get pod %s -n %s -o jsonpath='{.spec.containers[*].name}'", podName, sc.namespace),
			out+"\n", "",
		)
	}
	return nil
}
