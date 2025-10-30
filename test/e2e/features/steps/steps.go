package steps

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/cucumber/godog"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

const (
	olmNamespace      = "olmv1-system"
	olmDeploymentName = "operator-controller-controller-manager"
	timeout           = 300 * time.Second
	tick              = 1 * time.Second
)

var kubeconfigPath string

func init() {
	kubeconfigPath = os.Getenv("HOME") + "/.kube/config"
}

func kubectl(args ...string) (string, error) {
	cmd := exec.Command("kubectl", args...)
	fmt.Println(strings.Join(cmd.Args, " "))
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))
	b, err := cmd.Output()
	return string(b), err
}

func kubectlWithInput(yaml string, args ...string) (string, error) {
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = bytes.NewBufferString(yaml)
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))
	b, err := cmd.Output()
	return string(b), err
}

func OLMisAvailable(ctx context.Context) error {
	require.Eventually(godog.T(ctx), func() bool {
		v, err := kubectl("get", "deployment", "-n", olmNamespace, olmDeploymentName, "-o", "jsonpath='{.status.conditions[?(@.type==\"Available\")].status}'")
		if err != nil {
			return false
		}
		return v == "'True'"
	}, timeout, tick)
	return nil
}

func BundleInstalled(ctx context.Context, name, version string) error {
	sc := scenarioCtx(ctx)
	return waitFor(ctx, func() bool {
		v, err := kubectl("get", "clusterextension", sc.clusterExtensionName, "-o", "jsonpath={.status.install.bundle}")
		if err != nil {
			return false
		}
		var bundle map[string]interface{}
		if err := json.Unmarshal([]byte(v), &bundle); err != nil {
			return false
		}
		return bundle["name"] == name && bundle["version"] == version
	})
}

func toUnstructured(yamlContent string) (*unstructured.Unstructured, error) {
	var u map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &u); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: u}, nil
}

func substituteScenarioVars(content string, sc *scenarioContext) string {
	result := strings.ReplaceAll(content, "$TEST_NAMESPACE", sc.namespace)
	result = strings.ReplaceAll(result, "$NAME", sc.clusterExtensionName)
	return result
}

func ResourceApplyFails(ctx context.Context, errMsg string, yamlTemplate *godog.DocString) error {
	sc := scenarioCtx(ctx)
	yamlContent := substituteScenarioVars(yamlTemplate.Content, sc)
	_, err := toUnstructured(yamlContent)
	if err != nil {
		return fmt.Errorf("failed to parse resource yaml: %v", err)
	}
	waitFor(ctx, func() bool {
		_, err := kubectlWithInput(yamlContent, "apply", "-f", "-")
		if err == nil {
			return false
			//return fmt.Errorf("expected apply to fail, got: %s", out)
		}
		if stdErr := string(err.(*exec.ExitError).Stderr); !strings.Contains(stdErr, errMsg) {
			return false
			//return fmt.Errorf("expected error message %s to be in stderr, got: %s", errMsg, stdErr)
		}
		return true
	})
	return nil
}

func ResourceIsApplied(ctx context.Context, yamlTemplate *godog.DocString) error {
	sc := scenarioCtx(ctx)
	yamlContent := substituteScenarioVars(yamlTemplate.Content, sc)
	res, err := toUnstructured(yamlContent)
	if err != nil {
		return fmt.Errorf("failed to parse resource yaml: %v", err)
	}
	out, err := kubectlWithInput(yamlContent, "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply resource %v %w", out, err)
	}
	if res.GetKind() == "ClusterExtension" {
		sc.clusterExtensionName = res.GetName()
	}
	return nil
}

func ClusterExtensionIsAvailable(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	require.Eventually(godog.T(ctx), func() bool {
		v, err := kubectl("get", "clusterextension", sc.clusterExtensionName, "-o", "jsonpath={.status.conditions[?(@.type==\"Installed\")].status}")
		if err != nil {
			return false
		}
		return v == "True"
	}, timeout, tick)
	return nil
}

func ClusterExtensionIsRolledOut(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	require.Eventually(godog.T(ctx), func() bool {
		v, err := kubectl("get", "clusterextension", sc.clusterExtensionName, "-o", "jsonpath={.status.conditions[?(@.type==\"Progressing\")]}")
		if err != nil {
			return false
		}

		var condition map[string]interface{}
		if err := json.Unmarshal([]byte(v), &condition); err != nil {
			return false
		}
		return condition["status"] == "True" && condition["reason"] == "Succeeded" && condition["type"] == "Progressing"
	}, timeout, tick)
	return nil
}

func waitFor(ctx context.Context, conditionFn func() bool) error {
	require.Eventually(godog.T(ctx), conditionFn, timeout, tick)
	return nil
}

func waitForExtensionCondition(ctx context.Context, conditionType, conditionStatus string, conditionReason *string, msg *string) error {
	sc := scenarioCtx(ctx)
	require.Eventually(godog.T(ctx), func() bool {
		v, err := kubectl("get", "clusterextension", sc.clusterExtensionName, "-o", fmt.Sprintf("jsonpath={.status.conditions[?(@.type==\"%s\")]}", conditionType))
		if err != nil {
			return false
		}

		var condition map[string]interface{}
		if err := json.Unmarshal([]byte(v), &condition); err != nil {
			return false
		}
		if condition["status"] != conditionStatus {
			return false
		}
		if conditionReason != nil && condition["reason"] != *conditionReason {
			return false
		}
		if msg != nil && condition["message"] != *msg {
			return false
		}

		return true
	}, timeout, tick)
	return nil
}

func ClusterExtensionReportsCondition(ctx context.Context, conditionType, conditionStatus, conditionReason string, msg *godog.DocString) error {
	var conditionMsg *string
	if msg != nil {
		conditionMsg = ptr.To(substituteScenarioVars(strings.Join(strings.Fields(msg.Content), " "), scenarioCtx(ctx)))
	}
	return waitForExtensionCondition(ctx, conditionType, conditionStatus, &conditionReason, conditionMsg)
}

func ClusterExtensionReportsConditionWithoutMsg(ctx context.Context, conditionType, conditionStatus, conditionReason string) error {
	return ClusterExtensionReportsCondition(ctx, conditionType, conditionStatus, conditionReason, nil)
}

func ClusterExtensionReportsConditionWithoutReason(ctx context.Context, conditionType, conditionStatus string) error {
	return waitForExtensionCondition(ctx, conditionType, conditionStatus, nil, nil)
}

func ResourceAvailable(ctx context.Context, resource string) error {
	sc := scenarioCtx(ctx)
	resource = substituteScenarioVars(resource, sc)
	rtype, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <type>/<name>", resource)
	}
	return waitFor(ctx, func() bool {
		_, err := kubectl("get", rtype, name, "-n", sc.namespace)
		if err != nil {
			return false
		}
		return true
	})
}

func ResourceRemoved(ctx context.Context, resource string) error {
	sc := scenarioCtx(ctx)
	rtype, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <type>/<name>", resource)
	}
	yaml, err := kubectl("get", rtype, name, "-n", sc.namespace, "-o", "yaml")
	if err != nil {
		return err
	}
	obj, err := toUnstructured(yaml)
	if err != nil {
		return err
	}
	sc.removedResources = append(sc.removedResources, *obj)
	_, err = kubectl("delete", rtype, name, "-n", sc.namespace)
	return err
}

func ResourceMatches(ctx context.Context, resource string, requiredContentTemplate *godog.DocString) error {
	sc := scenarioCtx(ctx)
	resource = substituteScenarioVars(resource, sc)
	rtype, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <type>/<name>", resource)
	}
	requiredContent, err := toUnstructured(substituteScenarioVars(requiredContentTemplate.Content, sc))
	if err != nil {
		return fmt.Errorf("failed to parse required resource yaml: %v", err)
	}
	return waitFor(ctx, func() bool {
		objJson, err := kubectl("get", rtype, name, "-n", sc.namespace, "-o", "json")
		if err != nil {
			return false
		}
		obj, err := toUnstructured(objJson)
		if err != nil {
			return false
		}
		patch, err := json.Marshal(requiredContent.Object)
		if err != nil {
			return false
		}
		updJson, err := jsonpatch.MergePatch([]byte(objJson), patch)
		if err != nil {
			return false
		}
		upd, err := toUnstructured(string(updJson))
		if err != nil {
			return false
		}

		return len(cmp.Diff(upd.Object, obj.Object)) == 0
	})
}

func ResourceRestored(ctx context.Context, resource string) error {
	sc := scenarioCtx(ctx)
	rtype, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <type>/<name>", resource)
	}
	return waitFor(ctx, func() bool {
		yaml, err := kubectl("get", rtype, name, "-n", sc.namespace, "-o", "yaml")
		if err != nil {
			return false
		}
		obj, err := toUnstructured(yaml)
		if err != nil {
			return false
		}
		ct := obj.GetCreationTimestamp()

		for i, removed := range sc.removedResources {
			rct := removed.GetCreationTimestamp()
			if removed.GetName() == obj.GetName() && removed.GetKind() == obj.GetKind() && rct.Before(&ct) {
				switch rtype {
				case "configmap":
					if !reflect.DeepEqual(removed.Object["data"], obj.Object["data"]) {
						return false
					}
				default:
					if !reflect.DeepEqual(removed.Object["spec"], obj.Object["spec"]) {
						return false
					}
				}
				sc.removedResources = append(sc.removedResources[:i], sc.removedResources[i+1:]...)
				return true
			}
		}
		return false
	})
}

func applyPermissionsToServiceAccount(ctx context.Context, serviceAccount, rbacTemplate string, keyValue ...string) error {
	sc := scenarioCtx(ctx)
	yamlContent, err := os.ReadFile(filepath.Join("features", "steps", "testdata", rbacTemplate))
	if err != nil {
		return fmt.Errorf("failed to read RBAC template yaml: %v", err)
	}

	// Replace template variables
	yaml := string(yamlContent)
	yaml = strings.ReplaceAll(yaml, "{namespace}", sc.namespace)
	yaml = strings.ReplaceAll(yaml, "{serviceaccount_name}", serviceAccount)
	yaml = strings.ReplaceAll(yaml, "{clusterextension_name}", sc.clusterExtensionName)
	if len(keyValue) > 0 {
		for i := 0; i < len(keyValue); i += 2 {
			yaml = strings.ReplaceAll(yaml, fmt.Sprintf("{%s}", keyValue[i]), keyValue[i+1])
		}
	}

	// Apply the RBAC configuration
	_, err = kubectlWithInput(yaml, "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply RBAC configuration: %v", err)
	}

	return nil
}

func ServiceAccountWithNeededPermissionsIsAvailableInNamespace(ctx context.Context, serviceAccount string) error {
	return applyPermissionsToServiceAccount(ctx, serviceAccount, "rbac-template.yaml")
}

func ServiceAccountWithClusterAdminPermissionsIsAvailableInNamespace(ctx context.Context, serviceAccount string) error {
	return applyPermissionsToServiceAccount(ctx, serviceAccount, "cluster-admin-rbac-template.yaml")
}

func ServiceAccountWithFetchMetricsPermissions(ctx context.Context, serviceAccount string, controllerName string) error {
	return applyPermissionsToServiceAccount(ctx, serviceAccount, "metrics-reader-rbac-template.yaml", "controller_name", controllerName)
}

func httpGet(url string, token string) (*http.Response, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func SendMetricsRequest(ctx context.Context, serviceAccount string, endpoint string, controllerName string) error {
	sc := scenarioCtx(ctx)
	portForwardCmd := exec.Command("kubectl", "port-forward", "-n", olmNamespace, fmt.Sprintf("service/%s-service", controllerName), "8443:metrics")
	sc.backGroundCmds = append(sc.backGroundCmds, portForwardCmd)
	if err := portForwardCmd.Start(); err != nil {
		return err
	}
	token, err := kubectl("create", "token", serviceAccount, "-n", sc.namespace)
	if err != nil {
		return err
	}
	waitFor(ctx, func() bool {
		resp, err := httpGet(fmt.Sprintf("https://localhost:8443%s", endpoint), token)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				return false
			}
			sc.metricsResponse = string(b)
			return true
		}
		return false
	})

	return nil
}

func CatalogIsUpdatedToVersion(name, version string) error {
	ref, err := kubectl("get", "clustercatalog", fmt.Sprintf("%s-catalog", name), "-o", "jsonpath={.spec.source.image.ref}")
	if err != nil {
		return err
	}
	i := strings.LastIndexByte(ref, ':')
	if i == -1 {
		return fmt.Errorf("failed to find tag in image reference %s", ref)
	}
	base := ref[:i]
	patch := map[string]any{
		"spec": map[string]any{
			"source": map[string]any{
				"image": map[string]any{
					"ref": fmt.Sprintf("%s:%s", base, version),
				},
			},
		},
	}
	pb, err := json.Marshal(patch)
	if err != nil {
	}
	_, err = kubectl("patch", "clustercatalog", fmt.Sprintf("%s-catalog", name), "--type", "merge", "-p", string(pb))
	return err
}

func CatalogServesBundles(ctx context.Context, catalogName string) error {
	yamlContent, err := os.ReadFile(filepath.Join("features", "steps", "testdata", fmt.Sprintf("%s-catalog.yaml", catalogName)))
	if err != nil {
		return fmt.Errorf("failed to read catalog yaml: %v", err)
	}

	_, err = kubectlWithInput(string(yamlContent), "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply catalog: %v", err)
	}

	return nil
}

func TagCatalogImage(name, oldTag, newTag string) error {
	imageRef := fmt.Sprintf("%s/%s", os.Getenv("LOCAL_REGISTRY_HOST"), fmt.Sprintf("e2e/%s-catalog:%s", name, oldTag))
	return crane.Tag(imageRef, newTag, crane.Insecure)
}

func PrometheusMetricsAreReturned(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	if sc.metricsResponse == "" {
		return fmt.Errorf("metrics response is empty")
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	metricsFamilies, err := parser.TextToMetricFamilies(strings.NewReader(sc.metricsResponse))
	if err != nil {
		return fmt.Errorf("failed to parse metrics response: %v", err)
	}
	if len(metricsFamilies) == 0 {
		return fmt.Errorf("metrics response does not contain any metrics")
	}
	return nil
}

func OperatorTargetNamespace(ctx context.Context, operator, namespace string) error {
	sc := scenarioCtx(ctx)
	namespace = substituteScenarioVars(namespace, sc)
	raw, err := kubectl("get", "deployment", "-n", sc.namespace, operator, "-o", "json")
	if err != nil {
		return err
	}
	d := &appsv1.Deployment{}
	if err := json.Unmarshal([]byte(raw), d); err != nil {
		return err
	}

	if tns := d.Spec.Template.Annotations["olm.targetNamespaces"]; tns != namespace {
		return fmt.Errorf("expected target namespace %s, got %s", namespace, tns)
	}
	return nil
}
