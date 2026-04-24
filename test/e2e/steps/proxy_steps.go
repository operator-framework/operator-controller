package steps

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
)

// ---------------------------------------------------------------------------
// recordingProxy — an in-process HTTP CONNECT proxy that tunnels connections
// and records the host of every CONNECT request it receives.
// ---------------------------------------------------------------------------

type recordingProxy struct {
	listener net.Listener
	mu       sync.Mutex
	hosts    []string
}

func newRecordingProxy() (*recordingProxy, error) {
	l, err := net.Listen("tcp", "0.0.0.0:0") //nolint:gosec // must bind to all interfaces so cluster pods can reach the host
	if err != nil {
		return nil, fmt.Errorf("failed to start recording proxy: %w", err)
	}
	p := &recordingProxy{listener: l}
	go p.serve()
	return p, nil
}

func (p *recordingProxy) addr() string {
	return p.listener.Addr().String()
}

func (p *recordingProxy) port() (string, error) {
	_, port, err := net.SplitHostPort(p.addr())
	if err != nil {
		return "", fmt.Errorf("failed to parse proxy address %q: %w", p.addr(), err)
	}
	return port, nil
}

func (p *recordingProxy) serve() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return // listener closed
		}
		go p.handle(conn)
	}
}

func (p *recordingProxy) handle(conn net.Conn) {
	defer conn.Close()

	// Use a buffered reader so http.ReadRequest can parse the full request
	// even if headers arrive across multiple TCP segments.
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	if req.Method != http.MethodConnect {
		_, _ = conn.Write([]byte("HTTP/1.1 405 Method Not Allowed\r\n\r\n"))
		return
	}
	target := req.Host

	p.mu.Lock()
	p.hosts = append(p.hosts, target)
	p.mu.Unlock()

	dst, err := (&net.Dialer{Timeout: 30 * time.Second}).Dial("tcp", target)
	if err != nil {
		_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer dst.Close()

	_, _ = conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))

	// Tunnel traffic in both directions.  Use br (not conn) as the source for
	// the client→server direction so that any bytes buffered after the CONNECT
	// headers are forwarded to the destination instead of being discarded.
	done := make(chan struct{}, 2)
	tunnel := func(dst io.Writer, src io.Reader) {
		defer func() { done <- struct{}{} }()
		_, _ = io.Copy(dst, src)
		// Half-close the write side so the other direction sees EOF and
		// its io.Copy returns, preventing the goroutine from hanging.
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}
	go tunnel(dst, br)
	go tunnel(conn, dst)
	<-done
	<-done // wait for both directions to finish before closing connections
}

func (p *recordingProxy) stop() {
	_ = p.listener.Close()
}

func (p *recordingProxy) recordedHosts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.hosts))
	copy(out, p.hosts)
	return out
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// kindGatewayIP returns the gateway IP of the "kind" network for the
// configured container runtime, which is the address that pods inside the
// kind cluster use to reach the host machine.  The runtime is read from the
// CONTAINER_RUNTIME environment variable; it defaults to "docker".
func kindGatewayIP() (string, error) {
	runtime := os.Getenv("CONTAINER_RUNTIME")
	if runtime == "" {
		runtime = "docker"
	}
	// Range over all IPAM config entries rather than hard-coding index 0.
	// Some kind setups place an IPv6 config at index 0 (with no Gateway) and
	// the IPv4 config at index 1; indexing 0 directly would return empty.
	out, err := exec.Command(runtime, "network", "inspect", "kind", //nolint:gosec
		"--format", "{{range .IPAM.Config}}{{.Gateway}} {{end}}").Output()
	if err != nil {
		return "", fmt.Errorf("failed to inspect kind %s network: %w", runtime, err)
	}
	// Prefer the first valid IPv4 gateway.
	for _, candidate := range strings.Fields(string(out)) {
		if candidate == "<no value>" {
			continue
		}
		if ip := net.ParseIP(candidate); ip != nil && ip.To4() != nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("kind %s network has no IPv4 gateway configured", runtime)
}

// kubernetesClusterIP returns the cluster IP of the "kubernetes" service in
// the "default" namespace, which is the address client-go uses to reach the
// API server from inside a pod (via the KUBERNETES_SERVICE_HOST env var).
func kubernetesClusterIP() (string, error) {
	ip, err := k8sClient("get", "service", "kubernetes", "-n", "default",
		"-o", "jsonpath={.spec.clusterIP}")
	if err != nil {
		return "", fmt.Errorf("failed to get kubernetes service cluster IP: %w", err)
	}
	return strings.TrimSpace(ip), nil
}

// getDeploymentContainerEnv returns the environment variables for the named
// container in the given deployment, as a slice of "NAME=VALUE" strings.
func getDeploymentContainerEnv(deploymentName, namespace, containerName string) ([]string, error) {
	raw, err := k8sClient("get", "deployment", deploymentName, "-n", namespace, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, deploymentName, err)
	}

	var dep appsv1.Deployment
	if err := json.Unmarshal([]byte(raw), &dep); err != nil {
		return nil, fmt.Errorf("failed to unmarshal deployment: %w", err)
	}

	for _, c := range dep.Spec.Template.Spec.Containers {
		if c.Name == containerName {
			env := make([]string, 0, len(c.Env))
			for _, e := range c.Env {
				env = append(env, e.Name+"="+e.Value)
			}
			return env, nil
		}
	}
	return nil, fmt.Errorf("container %q not found in deployment %s/%s", containerName, namespace, deploymentName)
}

// setDeploymentEnvVars replaces the environment of the named container with
// the provided "NAME=VALUE" pairs and waits for the rollout to complete.
// It locates the container by name (rather than assuming index 0) and uses
// the JSON Patch "add" operation, which creates the env field if absent.
func setDeploymentEnvVars(deploymentName, namespace, containerName string, env []string) error {
	// Fetch the deployment to find the container index.
	raw, err := k8sClient("get", "deployment", deploymentName, "-n", namespace, "-o", "json")
	if err != nil {
		return fmt.Errorf("failed to get deployment %s/%s: %w", namespace, deploymentName, err)
	}
	var dep appsv1.Deployment
	if err := json.Unmarshal([]byte(raw), &dep); err != nil {
		return fmt.Errorf("failed to unmarshal deployment: %w", err)
	}
	idx := -1
	for i, c := range dep.Spec.Template.Spec.Containers {
		if c.Name == containerName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("container %q not found in deployment %s/%s", containerName, namespace, deploymentName)
	}

	type envVar struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	envVars := make([]envVar, 0, len(env))
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid env var %q: must be NAME=VALUE", kv)
		}
		envVars = append(envVars, envVar{Name: parts[0], Value: parts[1]})
	}

	// Use "add" rather than "replace": "add" on an object key creates it if
	// absent and overwrites it if present, so it works whether the container
	// already has an env field or not.
	patch := []map[string]interface{}{
		{
			"op":    "add",
			"path":  fmt.Sprintf("/spec/template/spec/containers/%d/env", idx),
			"value": envVars,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	if _, err := k8sClient("patch", "deployment", deploymentName, "-n", namespace,
		"--type=json", fmt.Sprintf("--patch=%s", string(patchBytes))); err != nil {
		return fmt.Errorf("failed to patch deployment %s/%s: %w", namespace, deploymentName, err)
	}

	if _, err := k8sClient("rollout", "status", "deployment", deploymentName, "-n", namespace,
		"--timeout=5m"); err != nil {
		return fmt.Errorf("rollout of deployment %s/%s did not complete: %w", namespace, deploymentName, err)
	}

	return nil
}

// addOrReplaceEnvVar adds a "NAME=VALUE" entry to env, replacing an existing
// entry with the same NAME if present.
func addOrReplaceEnvVar(env []string, name, value string) []string {
	prefix := name + "="
	for i, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			result := make([]string, len(env))
			copy(result, env)
			result[i] = name + "=" + value
			return result
		}
	}
	return append(env, name+"="+value)
}

// restoreDeployment restores a deployment to its state captured in a
// deploymentRestore record.  It is called from ScenarioCleanup.
func restoreDeployment(r deploymentRestore) error {
	if r.originalEnv == nil {
		return nil
	}
	return setDeploymentEnvVars(r.name, r.namespace, r.containerName, r.originalEnv)
}

// configureDeploymentProxy patches the named deployment to set HTTPS_PROXY to
// proxyURL and NO_PROXY to the Kubernetes API server cluster IP, then waits
// for the rollout.  The original env is saved for cleanup.
func configureDeploymentProxy(ctx context.Context, component, proxyURL string) error {
	sc := scenarioCtx(ctx)

	var deployName string
	switch component {
	case "operator-controller":
		deployName = olmDeploymentName
	case "catalogd":
		deployName = "catalogd-controller-manager"
	default:
		return fmt.Errorf("unknown component %q", component)
	}

	// Only record an env restore entry the first time this deployment's env is
	// patched in this scenario.  Subsequent env patches must not overwrite the
	// saved original, otherwise cleanup would restore to an intermediate state.
	// We check for containerName != "" to distinguish env-restore records from
	// args-restore records (TLS profile patches), which share the same name and
	// namespace fields but have no containerName or originalEnv set.
	alreadyTracked := false
	for _, r := range sc.deploymentRestores {
		if r.name == deployName && r.namespace == olmNamespace && r.containerName != "" {
			alreadyTracked = true
			break
		}
	}

	origEnv, err := getDeploymentContainerEnv(deployName, olmNamespace, "manager")
	if err != nil {
		return err
	}

	if !alreadyTracked {
		sc.deploymentRestores = append(sc.deploymentRestores, deploymentRestore{
			name:          deployName,
			namespace:     olmNamespace,
			containerName: "manager",
			originalEnv:   origEnv,
		})
	}

	// Exclude the Kubernetes API server from proxying so the controller can
	// still reconcile resources.  client-go connects to KUBERNETES_SERVICE_HOST
	// which is the cluster IP of the "kubernetes" service — a plain IP, not a
	// DNS name, so DNS-wildcard NO_PROXY entries won't match it.
	k8sIP, err := kubernetesClusterIP()
	if err != nil {
		return err
	}

	newEnv := addOrReplaceEnvVar(origEnv, "HTTPS_PROXY", proxyURL)
	newEnv = addOrReplaceEnvVar(newEnv, "NO_PROXY", k8sIP)
	return setDeploymentEnvVars(deployName, olmNamespace, "manager", newEnv)
}

// ---------------------------------------------------------------------------
// Step functions
// ---------------------------------------------------------------------------

// ConfigureDeploymentWithHTTPSProxy sets HTTPS_PROXY to a dead loopback
// address on the given deployment, proving that catalog fetches are blocked
// when the proxy is unreachable.
func ConfigureDeploymentWithHTTPSProxy(ctx context.Context, component, proxyURL string) error {
	return configureDeploymentProxy(ctx, component, proxyURL)
}

// StartRecordingProxyAndConfigureDeployment starts an in-process HTTP CONNECT
// proxy reachable from the cluster via the container-runtime kind network
// gateway, then patches the component deployment to route HTTPS through it.
func StartRecordingProxyAndConfigureDeployment(ctx context.Context, component string) error {
	sc := scenarioCtx(ctx)

	p, err := newRecordingProxy()
	if err != nil {
		return err
	}
	sc.proxy = p

	port, err := p.port()
	if err != nil {
		return err
	}

	gatewayIP, err := kindGatewayIP()
	if err != nil {
		return fmt.Errorf("cannot reach host from cluster: %w", err)
	}

	proxyURL := fmt.Sprintf("http://%s:%s", gatewayIP, port)
	logger.Info("Recording proxy started", "url", proxyURL)

	return configureDeploymentProxy(ctx, component, proxyURL)
}

// RecordingProxyReceivedCONNECTForCatalogd polls until the recording proxy has
// received at least one CONNECT request whose target host contains "catalogd",
// or the polling timeout is reached.
//
// Note: the recording proxy runs on the host and cannot route to in-cluster
// service addresses, so it responds with 502 Bad Gateway after recording the
// CONNECT.  This is intentional — the step only verifies that operator-controller
// respected HTTPS_PROXY and sent the request through the proxy.
func RecordingProxyReceivedCONNECTForCatalogd(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	if sc.proxy == nil {
		return fmt.Errorf("no recording proxy was started in this scenario")
	}

	waitFor(ctx, func() bool {
		for _, h := range sc.proxy.recordedHosts() {
			if strings.Contains(h, "catalogd") {
				logger.Info("Recording proxy confirmed CONNECT for catalogd", "host", h)
				return true
			}
		}
		return false
	})

	return nil
}
