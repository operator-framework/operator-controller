package steps

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// tlsCipherSuiteByName is a name→ID map that covers TLS 1.3 cipher suites
// (not returned by tls.CipherSuites) as well as all TLS 1.2 suites.
var tlsCipherSuiteByName = func() map[string]uint16 {
	m := map[string]uint16{
		// TLS 1.3 suites are not included in tls.CipherSuites(); add them explicitly.
		"TLS_AES_128_GCM_SHA256":       tls.TLS_AES_128_GCM_SHA256,
		"TLS_AES_256_GCM_SHA384":       tls.TLS_AES_256_GCM_SHA384,
		"TLS_CHACHA20_POLY1305_SHA256": tls.TLS_CHACHA20_POLY1305_SHA256,
	}
	for _, c := range tls.CipherSuites() {
		m[c.Name] = c.ID
	}
	for _, c := range tls.InsecureCipherSuites() {
		m[c.Name] = c.ID
	}
	return m
}()

// tlsCipherSuiteName returns a human-readable name for a cipher suite ID,
// including TLS 1.3 suites that tls.CipherSuiteName does not recognise.
func tlsCipherSuiteName(id uint16) string {
	for name, cid := range tlsCipherSuiteByName {
		if cid == id {
			return name
		}
	}
	return fmt.Sprintf("0x%04X", id)
}

// curveIDByName maps the curve names used in --tls-custom-curves flags to Go CurveID values.
var curveIDByName = map[string]tls.CurveID{
	"X25519MLKEM768": tls.X25519MLKEM768,
	"X25519":         tls.X25519,
	"prime256v1":     tls.CurveP256,
	"secp384r1":      tls.CurveP384,
	"secp521r1":      tls.CurveP521,
}

// getMetricsServiceEndpoint returns the namespace and metrics port for the named component service.
func getMetricsServiceEndpoint(component string) (string, int32, error) {
	serviceName := fmt.Sprintf("%s-service", component)
	serviceNs, err := k8sClient("get", "service", "-A", "-o",
		fmt.Sprintf(`jsonpath={.items[?(@.metadata.name=="%s")].metadata.namespace}`, serviceName))
	if err != nil {
		return "", 0, fmt.Errorf("failed to find namespace for service %s: %w", serviceName, err)
	}
	serviceNs = strings.TrimSpace(serviceNs)
	if serviceNs == "" {
		return "", 0, fmt.Errorf("service %s not found in any namespace", serviceName)
	}

	raw, err := k8sClient("get", "service", "-n", serviceNs, serviceName, "-o", "json")
	if err != nil {
		return "", 0, fmt.Errorf("failed to get service %s: %w", serviceName, err)
	}
	var svc corev1.Service
	if err := json.Unmarshal([]byte(raw), &svc); err != nil {
		return "", 0, fmt.Errorf("failed to unmarshal service %s: %w", serviceName, err)
	}
	for _, p := range svc.Spec.Ports {
		if p.Name == "metrics" {
			return serviceNs, p.Port, nil
		}
	}
	return "", 0, fmt.Errorf("no port named 'metrics' found on service %s", serviceName)
}

// withMetricsPortForward starts a kubectl port-forward to the component's metrics service,
// waits until a basic TLS connection succeeds (confirming the port-forward is ready),
// then calls fn with the local address. The port-forward is torn down when fn returns.
func withMetricsPortForward(ctx context.Context, component string, fn func(addr string) error) error {
	ns, metricsPort, err := getMetricsServiceEndpoint(component)
	if err != nil {
		return err
	}

	localPort, err := randomAvailablePort()
	if err != nil {
		return fmt.Errorf("failed to find a free local port: %w", err)
	}

	serviceName := fmt.Sprintf("%s-service", component)
	pfCmd := exec.Command(k8sCli, "port-forward", "-n", ns, //nolint:gosec
		fmt.Sprintf("service/%s", serviceName),
		fmt.Sprintf("%d:%d", localPort, metricsPort))
	pfCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))
	if err := pfCmd.Start(); err != nil {
		return fmt.Errorf("failed to start port-forward to %s: %w", serviceName, err)
	}
	defer func() {
		if p := pfCmd.Process; p != nil {
			_ = p.Kill()
			_ = pfCmd.Wait()
		}
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", localPort)

	// Wait until the port-forward is accepting connections. A plain TLS dial (no version
	// restrictions) serves as the readiness probe; any successful TLS handshake confirms
	// the tunnel is up.  A short per-attempt timeout prevents the dial from blocking
	// indefinitely if the local port is open but the upstream handshake stalls.
	waitFor(ctx, func() bool {
		dialer := &net.Dialer{Timeout: 3 * time.Second}
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		if err != nil {
			return false
		}
		conn.Close()
		return true
	})

	return fn(addr)
}

// MetricsEndpointAcceptsTLS13 verifies that the component's metrics endpoint accepts
// connections negotiated at TLS 1.3.
func MetricsEndpointAcceptsTLS13(ctx context.Context, component string) error {
	return withMetricsPortForward(ctx, component, func(addr string) error {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // self-signed cert in e2e
			MinVersion:         tls.VersionTLS13,
		})
		if err != nil {
			return fmt.Errorf("%s metrics endpoint rejected a TLS 1.3 connection: %w", component, err)
		}
		conn.Close()
		return nil
	})
}

// MetricsEndpointRejectsTLS12 verifies that the component's metrics endpoint refuses
// connections from clients that advertise TLS 1.2 as their maximum supported version.
func MetricsEndpointRejectsTLS12(ctx context.Context, component string) error {
	return withMetricsPortForward(ctx, component, func(addr string) error {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // self-signed cert in e2e
			MaxVersion:         tls.VersionTLS12,
		})
		if err == nil {
			conn.Close()
			return fmt.Errorf("%s metrics endpoint accepted a TLS 1.2 connection but its profile requires TLS 1.3", component)
		}
		return nil
	})
}

// MetricsEndpointAcceptsConnectionUsingCurve verifies that the component's metrics
// endpoint accepts a connection from a client restricted to a single named curve.
func MetricsEndpointAcceptsConnectionUsingCurve(ctx context.Context, component, curveName string) error {
	curveID, ok := curveIDByName[curveName]
	if !ok {
		return fmt.Errorf("unknown curve name %q", curveName)
	}
	return withMetricsPortForward(ctx, component, func(addr string) error {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // self-signed cert in e2e
			CurvePreferences:   []tls.CurveID{curveID},
		})
		if err != nil {
			return fmt.Errorf("%s metrics endpoint rejected a connection offering only curve %s: %w", component, curveName, err)
		}
		conn.Close()
		return nil
	})
}

// componentDeploymentName maps the short component name used in feature files to
// the actual Kubernetes Deployment name.
func componentDeploymentName(component string) (string, error) {
	switch component {
	case "operator-controller":
		return "operator-controller-controller-manager", nil
	case "catalogd":
		return "catalogd-controller-manager", nil
	default:
		return "", fmt.Errorf("unknown component %q: expected operator-controller or catalogd", component)
	}
}

// getDeploymentContainerArgs returns the args list of the container named "manager"
// inside the named deployment.
func getDeploymentContainerArgs(ns, name string) ([]string, error) {
	raw, err := k8sClient("get", "deployment", name, "-n", ns, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("getting deployment %s/%s: %w", ns, name, err)
	}
	var deploy appsv1.Deployment
	if err := json.Unmarshal([]byte(raw), &deploy); err != nil {
		return nil, fmt.Errorf("parsing deployment %s/%s: %w", ns, name, err)
	}
	for _, c := range deploy.Spec.Template.Spec.Containers {
		if c.Name == "manager" {
			return c.Args, nil
		}
	}
	return nil, fmt.Errorf("no container named 'manager' in deployment %s/%s", ns, name)
}

// getDeploymentContainerIndex returns the index of the container named "manager"
// inside the named deployment.
func getDeploymentContainerIndex(ns, name string) (int, error) {
	raw, err := k8sClient("get", "deployment", name, "-n", ns, "-o", "json")
	if err != nil {
		return -1, fmt.Errorf("getting deployment %s/%s: %w", ns, name, err)
	}
	var deploy appsv1.Deployment
	if err := json.Unmarshal([]byte(raw), &deploy); err != nil {
		return -1, fmt.Errorf("parsing deployment %s/%s: %w", ns, name, err)
	}
	for i, c := range deploy.Spec.Template.Spec.Containers {
		if c.Name == "manager" {
			return i, nil
		}
	}
	return -1, fmt.Errorf("no container named 'manager' in deployment %s/%s", ns, name)
}

// patchDeploymentArgs replaces the args of the "manager" container in the named
// deployment using a JSON patch targeting that container's actual index.
func patchDeploymentArgs(ns, name string, args []string) error {
	idx, err := getDeploymentContainerIndex(ns, name)
	if err != nil {
		return err
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`[{"op":"replace","path":"/spec/template/spec/containers/%d/args","value":%s}]`, idx, string(argsJSON))
	_, err = k8sClient("patch", "deployment", name, "-n", ns, "--type=json", "-p", patch)
	return err
}

// buildCustomTLSArgs strips any existing --tls-profile / --tls-custom-* flags from
// baseArgs and appends a fresh custom-profile configuration.
func buildCustomTLSArgs(baseArgs []string, version, ciphers, curves string) []string {
	filtered := make([]string, 0, len(baseArgs)+4)
	for _, arg := range baseArgs {
		switch {
		case strings.HasPrefix(arg, "--tls-profile="),
			strings.HasPrefix(arg, "--tls-custom-version="),
			strings.HasPrefix(arg, "--tls-custom-ciphers="),
			strings.HasPrefix(arg, "--tls-custom-curves="):
			// drop — will be replaced below
		default:
			filtered = append(filtered, arg)
		}
	}
	filtered = append(filtered, "--tls-profile=custom", "--tls-custom-version="+version)
	if ciphers != "" {
		filtered = append(filtered, "--tls-custom-ciphers="+ciphers)
	}
	if curves != "" {
		filtered = append(filtered, "--tls-custom-curves="+curves)
	}
	return filtered
}

// configureDeploymentCustomTLS saves the current deployment args for cleanup,
// patches the deployment with a custom TLS profile, and waits for the rollout.
func configureDeploymentCustomTLS(ctx context.Context, component, version, ciphers, curves string) error {
	deploymentName, err := componentDeploymentName(component)
	if err != nil {
		return err
	}

	origArgs, err := getDeploymentContainerArgs(olmNamespace, deploymentName)
	if err != nil {
		return err
	}

	sc := scenarioCtx(ctx)
	sc.deploymentRestores = append(sc.deploymentRestores, deploymentRestore{
		namespace:      olmNamespace,
		deploymentName: deploymentName,
		originalArgs:   origArgs,
	})

	newArgs := buildCustomTLSArgs(origArgs, version, ciphers, curves)
	if err := patchDeploymentArgs(olmNamespace, deploymentName, newArgs); err != nil {
		return fmt.Errorf("patching %s with custom TLS args: %w", deploymentName, err)
	}

	waitFor(ctx, func() bool {
		_, err := k8sClient("rollout", "status", "-n", olmNamespace,
			fmt.Sprintf("deployment/%s", deploymentName), "--timeout=10s")
		return err == nil
	})
	return nil
}

// ConfigureDeploymentWithCustomTLSVersion configures the component deployment with a
// custom TLS profile that only sets the minimum TLS version (no cipher or curve override).
func ConfigureDeploymentWithCustomTLSVersion(ctx context.Context, component, version string) error {
	return configureDeploymentCustomTLS(ctx, component, version, "", "")
}

// ConfigureDeploymentWithCustomTLSFull configures the component deployment with a
// custom TLS profile specifying version, cipher suite list, and curve list.
func ConfigureDeploymentWithCustomTLSFull(ctx context.Context, component, version, ciphers, curves string) error {
	return configureDeploymentCustomTLS(ctx, component, version, ciphers, curves)
}

// MetricsEndpointNegotiatesTLS12Cipher connects to the metrics endpoint, forces TLS 1.2,
// restricts the client to a single cipher, and asserts that cipher is what was negotiated.
func MetricsEndpointNegotiatesTLS12Cipher(ctx context.Context, component, cipherName string) error {
	cipherID, ok := tlsCipherSuiteByName[cipherName]
	if !ok {
		return fmt.Errorf("unknown cipher %q", cipherName)
	}
	return withMetricsPortForward(ctx, component, func(addr string) error {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // self-signed cert in e2e
			MaxVersion:         tls.VersionTLS12,
			CipherSuites:       []uint16{cipherID},
		})
		if err != nil {
			return fmt.Errorf("%s rejected TLS 1.2 connection using cipher %s: %w", component, cipherName, err)
		}
		defer conn.Close()
		state := conn.ConnectionState()
		if state.CipherSuite != cipherID {
			return fmt.Errorf("%s negotiated cipher %s instead of the expected %s",
				component, tlsCipherSuiteName(state.CipherSuite), cipherName)
		}
		return nil
	})
}

// MetricsEndpointRejectsTLS12ConnectionWithCipher connects with TLS 1.2 and a single
// cipher that is NOT in the server's configured cipher list, expecting a handshake failure.
func MetricsEndpointRejectsTLS12ConnectionWithCipher(ctx context.Context, component, cipherName string) error {
	cipherID, ok := tlsCipherSuiteByName[cipherName]
	if !ok {
		return fmt.Errorf("unknown cipher %q", cipherName)
	}
	return withMetricsPortForward(ctx, component, func(addr string) error {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // self-signed cert in e2e
			MaxVersion:         tls.VersionTLS12,
			CipherSuites:       []uint16{cipherID},
		})
		if err == nil {
			conn.Close()
			return fmt.Errorf("%s accepted TLS 1.2 with cipher %s but should have rejected it (not in configured cipher list)", component, cipherName)
		}
		return nil
	})
}

// MetricsEndpointAcceptsTLS12ConnectionWithCurve connects with TLS 1.2, a specific cipher,
// and a single curve, asserting the connection succeeds (curve is in server's preferences).
func MetricsEndpointAcceptsTLS12ConnectionWithCurve(ctx context.Context, component, cipherName, curveName string) error {
	cipherID, ok := tlsCipherSuiteByName[cipherName]
	if !ok {
		return fmt.Errorf("unknown cipher %q", cipherName)
	}
	curveID, ok := curveIDByName[curveName]
	if !ok {
		return fmt.Errorf("unknown curve %q", curveName)
	}
	return withMetricsPortForward(ctx, component, func(addr string) error {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // self-signed cert in e2e
			MaxVersion:         tls.VersionTLS12,
			CipherSuites:       []uint16{cipherID},
			CurvePreferences:   []tls.CurveID{curveID},
		})
		if err != nil {
			return fmt.Errorf("%s rejected TLS 1.2 with cipher %s and curve %s: %w", component, cipherName, curveName, err)
		}
		conn.Close()
		return nil
	})
}

// MetricsEndpointRejectsTLS12ConnectionWithCurve connects with TLS 1.2, a specific cipher,
// and a single curve that is NOT in the server's curve preferences, expecting failure.
func MetricsEndpointRejectsTLS12ConnectionWithCurve(ctx context.Context, component, cipherName, curveName string) error {
	cipherID, ok := tlsCipherSuiteByName[cipherName]
	if !ok {
		return fmt.Errorf("unknown cipher %q", cipherName)
	}
	curveID, ok := curveIDByName[curveName]
	if !ok {
		return fmt.Errorf("unknown curve %q", curveName)
	}
	return withMetricsPortForward(ctx, component, func(addr string) error {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // self-signed cert in e2e
			MaxVersion:         tls.VersionTLS12,
			CipherSuites:       []uint16{cipherID},
			CurvePreferences:   []tls.CurveID{curveID},
		})
		if err == nil {
			conn.Close()
			return fmt.Errorf("%s accepted TLS 1.2 with cipher %s and curve %s but should have rejected it (curve not in configured preferences)", component, cipherName, curveName)
		}
		return nil
	})
}

// MetricsEndpointNegotiatesCipherIn connects to the component's metrics endpoint,
// completes a TLS handshake, and asserts that the negotiated cipher suite is one of
// the comma-separated names in cipherList.
//
// Note: Go's crypto/tls does not allow restricting TLS 1.3 cipher suites on either
// side of a connection; the suite is chosen by the server based on AES hardware
// availability (TLS_AES_128_GCM_SHA256 preferred with AES-NI,
// TLS_CHACHA20_POLY1305_SHA256 otherwise). This step therefore validates observed
// negotiation behaviour rather than server-side enforcement.
func MetricsEndpointNegotiatesCipherIn(ctx context.Context, component, cipherList string) error {
	expectedIDs := map[uint16]bool{}
	for _, name := range strings.Split(cipherList, ",") {
		name = strings.TrimSpace(name)
		id, ok := tlsCipherSuiteByName[name]
		if !ok {
			return fmt.Errorf("unknown cipher name %q in expected list", name)
		}
		expectedIDs[id] = true
	}

	return withMetricsPortForward(ctx, component, func(addr string) error {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // self-signed cert in e2e
		})
		if err != nil {
			return fmt.Errorf("failed to connect to %s metrics endpoint: %w", component, err)
		}
		defer conn.Close()

		state := conn.ConnectionState()
		if !expectedIDs[state.CipherSuite] {
			return fmt.Errorf("%s negotiated cipher %s, which is not in the expected set [%s]",
				component, tlsCipherSuiteName(state.CipherSuite), cipherList)
		}
		return nil
	})
}

// MetricsEndpointRejectsConnectionUsingOnlyCurve verifies that the component's metrics
// endpoint refuses a connection from a client whose only supported curve is not in
// the server's configured curve preferences.
func MetricsEndpointRejectsConnectionUsingOnlyCurve(ctx context.Context, component, curveName string) error {
	curveID, ok := curveIDByName[curveName]
	if !ok {
		return fmt.Errorf("unknown curve name %q", curveName)
	}
	return withMetricsPortForward(ctx, component, func(addr string) error {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // self-signed cert in e2e
			CurvePreferences:   []tls.CurveID{curveID},
		})
		if err == nil {
			conn.Close()
			return fmt.Errorf("%s metrics endpoint accepted a connection offering only curve %s, but that curve is not in its configured preferences", component, curveName)
		}
		return nil
	})
}
