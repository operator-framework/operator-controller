package hardcoded

import (
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

var HardcodedEntitySource = input.NewCacheQuerier(map[deppy.Identifier]input.Entity{
	"operatorhub/prometheus/0.14.0": *input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:c78cc60ad05445f423c66e37c464bc9f520f0c0741cfd351b4f839ae0b99bd4b"`,
		"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0}",
		"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"}]",
		"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.14.0\"}",
	}),
	"operatorhub/prometheus/0.15.0": *input.NewEntity("operatorhub/prometheus/0.15.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:0a5a122cef6fabebcb82122bb4f5c4fbfa205454d109987b8af71672c8ac5c0e"`,
		"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.14.0\"}",
		"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"}]",
		"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.15.0\"}",
	}),
	"operatorhub/prometheus/0.22.2": *input.NewEntity("operatorhub/prometheus/0.22.2", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:f24b92e70ffb3bf33cc2a142f8b7a6519d28c90aa5742ddd37ac4fcecb5e5a52"`,
		"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.15.0\"}",
		"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PrometheusRule\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PrometheusRule\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"}]",
		"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.22.2\"}",
	}),
	"operatorhub/prometheus/0.27.0": *input.NewEntity("operatorhub/prometheus/0.27.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:7aace7b24fa2587c61d37d8676e23ea24dce03f1751b94614c6af60fba364f63"`,
		"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.22.2\"}",
		"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PrometheusRule\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PrometheusRule\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"}]",
		"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.27.0\"}",
	}),
	"operatorhub/prometheus/0.32.0": *input.NewEntity("operatorhub/prometheus/0.32.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:14f75077f01feab351f7a046ccfcff6ad357bed2393048d0f6a41f6e64c63278"`,
		"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.27.0\"}",
		"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PodMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PodMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PrometheusRule\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PrometheusRule\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"}]",
		"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.32.0\"}",
	}),
	"operatorhub/prometheus/0.37.0": *input.NewEntity("operatorhub/prometheus/0.37.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35"`,
		"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.32.0\"}",
		"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PodMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PodMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PrometheusRule\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PrometheusRule\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ThanosRuler\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ThanosRuler\",\"version\":\"v1\"}]",
		"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.37.0\"}",
	}),
	"operatorhub/prometheus/0.47.0": *input.NewEntity("operatorhub/prometheus/0.47.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed"`,
		"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.37.0\"}",
		"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"AlertmanagerConfig\",\"version\":\"v1alpha1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"AlertmanagerConfig\",\"version\":\"v1alpha1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PodMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PodMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Probe\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Probe\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PrometheusRule\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"PrometheusRule\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ServiceMonitor\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ThanosRuler\",\"version\":\"v1\"},{\"group\":\"monitoring.coreos.com\",\"kind\":\"ThanosRuler\",\"version\":\"v1\"}]",
		"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.47.0\"}",
	}),
	"operatorhub/lightbend-console-operator/0.0.1": *input.NewEntity("operatorhub/lightbend-console-operator/0.0.1", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/lightbend-console-operator@sha256:2cf5f1abf71be29b7d2667ae9ca4102198c93cdef450d09faf1b26900443e285"`,
		"olm.channel":     "{\"channelName\":\"alpha\",\"priority\":0}",
		"olm.gvk":         "[{\"group\":\"app.lightbend.com\",\"kind\":\"Console\",\"version\":\"v1alpha1\"}]",
		"olm.package":     "{\"packageName\":\"lightbend-console-operator\",\"version\":\"0.0.1\"}",
	}),
})
