package main

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"testing/fstest"

	"github.com/operator-framework/operator-controller/internal/catalogd/server"
	"github.com/operator-framework/operator-controller/internal/catalogd/service"
)

// demoCatalogStore implements server.CatalogStore backed by an in-memory FS
type demoCatalogStore struct {
	catalogs map[string]fs.FS
}

func (s *demoCatalogStore) GetCatalogData(catalog string) (*os.File, os.FileInfo, error) {
	return nil, nil, fmt.Errorf("not implemented for demo")
}

func (s *demoCatalogStore) GetCatalogFS(catalog string) (fs.FS, error) {
	catFS, ok := s.catalogs[catalog]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return catFS, nil
}

func (s *demoCatalogStore) GetIndex(catalog string) (server.Index, error) {
	return nil, fmt.Errorf("not implemented for demo")
}

// dummyIndex satisfies the server.Index interface (unused in GraphQL flow)
type dummyIndex struct{}

func (d *dummyIndex) Get(_ io.ReaderAt, _, _, _ string) io.Reader { return nil }

func main() {
	addr := ":9376"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}

	store := &demoCatalogStore{
		catalogs: map[string]fs.FS{
			"redhat-operators": buildRedHatCatalog(),
		},
	}

	graphqlSvc := service.NewCachedGraphQLService()
	rootURL, _ := url.Parse("/catalogs/")
	handlers := server.NewCatalogHandlers(
		store,
		graphqlSvc,
		rootURL,
		server.MetasHandlerDisabled,
		server.GraphQLQueriesEnabled,
	)

	mux := http.NewServeMux()
	mux.Handle("/catalogs/", handlers.Handler())

	fmt.Fprintf(os.Stderr, "GraphQL demo server listening on http://localhost%s\n", addr)
	fmt.Fprintf(os.Stderr, "Endpoint: POST http://localhost%s/catalogs/redhat-operators/api/v1/graphql\n", addr)
	fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop.\n")

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Fprintln(os.Stderr, "\nShutting down.")
}

func buildRedHatCatalog() fs.FS {
	return fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(catalogJSON),
		},
	}
}

// catalogJSON contains realistic FBC data modeled after the Red Hat operator catalog.
// Each line is a separate JSON object (JSONL format parsed by WalkMetasFS).
const catalogJSON = `{"schema":"olm.package","name":"compliance-operator","defaultChannel":"stable","description":"An operator to run compliance scans and provide remediations."}
{"schema":"olm.package","name":"elasticsearch-operator","defaultChannel":"stable-v0","description":"Elasticsearch Operator for OCP provides a means for configuring and managing an Elasticsearch cluster for tracing and cluster logging."}
{"schema":"olm.package","name":"amq-streams","defaultChannel":"stable","description":"Apache Kafka on OpenShift via AMQ Streams operator."}
{"schema":"olm.package","name":"openshift-pipelines-operator-rh","defaultChannel":"latest","description":"Red Hat OpenShift Pipelines provides cloud-native CI/CD based on Tekton."}
{"schema":"olm.package","name":"servicemeshoperator","defaultChannel":"stable","description":"Red Hat OpenShift Service Mesh based on Istio."}
{"schema":"olm.channel","name":"stable","package":"compliance-operator","entries":[{"name":"compliance-operator.v1.6.0","skipRange":">=0.1.17 <1.6.0"},{"name":"compliance-operator.v1.7.0","replaces":"compliance-operator.v1.6.0","skipRange":">=0.1.17 <1.7.0"},{"name":"compliance-operator.v1.7.1","replaces":"compliance-operator.v1.7.0","skipRange":">=1.0.0 <1.7.1"}]}
{"schema":"olm.channel","name":"stable-v0","package":"elasticsearch-operator","entries":[{"name":"elasticsearch-operator.v5.8.10","skipRange":">=5.6.0 <5.8.10"},{"name":"elasticsearch-operator.v5.8.14","replaces":"elasticsearch-operator.v5.8.10","skipRange":">=5.6.0 <5.8.14"}]}
{"schema":"olm.channel","name":"stable","package":"amq-streams","entries":[{"name":"amqstreams.v2.7.0-0","skipRange":">=2.0.0 <2.7.0-0"},{"name":"amqstreams.v2.8.0-0","replaces":"amqstreams.v2.7.0-0","skipRange":">=2.0.0 <2.8.0-0"}]}
{"schema":"olm.channel","name":"latest","package":"openshift-pipelines-operator-rh","entries":[{"name":"openshift-pipelines-operator-rh.v1.14.4","skipRange":">=1.5.0 <1.14.4"},{"name":"openshift-pipelines-operator-rh.v1.15.1","replaces":"openshift-pipelines-operator-rh.v1.14.4","skipRange":">=1.5.0 <1.15.1"}]}
{"schema":"olm.channel","name":"stable","package":"servicemeshoperator","entries":[{"name":"servicemeshoperator.v2.5.2","skipRange":">=2.0.0 <2.5.2"},{"name":"servicemeshoperator.v2.6.2","replaces":"servicemeshoperator.v2.5.2","skipRange":">=2.0.0 <2.6.2"}]}
{"schema":"olm.bundle","name":"compliance-operator.v1.6.0","package":"compliance-operator","image":"registry.redhat.io/compliance/openshift-compliance-rhel8-operator@sha256:aaa111","properties":[{"type":"olm.package","value":{"packageName":"compliance-operator","version":"1.6.0"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceScan"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceSuite"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceRemediation"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"true","proxy":"true","tlsProfiles":"false","tokenAuthentication":"false"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/compliance/openshift-compliance-rhel8-operator@sha256:aaa111"},{"name":"openscap","image":"registry.redhat.io/compliance/openscap-rhel8@sha256:bbb222"}]}
{"schema":"olm.bundle","name":"compliance-operator.v1.7.0","package":"compliance-operator","image":"registry.redhat.io/compliance/openshift-compliance-rhel8-operator@sha256:ccc333","replaces":"compliance-operator.v1.6.0","properties":[{"type":"olm.package","value":{"packageName":"compliance-operator","version":"1.7.0"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceScan"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceSuite"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceRemediation"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceCheckResult"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"true","proxy":"true","tlsProfiles":"true","tokenAuthentication":"false"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/compliance/openshift-compliance-rhel8-operator@sha256:ccc333"},{"name":"openscap","image":"registry.redhat.io/compliance/openscap-rhel8@sha256:ddd444"}]}
{"schema":"olm.bundle","name":"compliance-operator.v1.7.1","package":"compliance-operator","image":"registry.redhat.io/compliance/openshift-compliance-rhel8-operator@sha256:eee555","replaces":"compliance-operator.v1.7.0","properties":[{"type":"olm.package","value":{"packageName":"compliance-operator","version":"1.7.1"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceScan"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceSuite"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceRemediation"}},{"type":"olm.gvk","value":{"group":"compliance.openshift.io","version":"v1alpha1","kind":"ComplianceCheckResult"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"true","proxy":"true","tlsProfiles":"true","tokenAuthentication":"false"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/compliance/openshift-compliance-rhel8-operator@sha256:eee555"},{"name":"openscap","image":"registry.redhat.io/compliance/openscap-rhel8@sha256:fff666"}]}
{"schema":"olm.bundle","name":"elasticsearch-operator.v5.8.10","package":"elasticsearch-operator","image":"registry.redhat.io/openshift-logging/elasticsearch-rhel9-operator@sha256:111aaa","properties":[{"type":"olm.package","value":{"packageName":"elasticsearch-operator","version":"5.8.10"}},{"type":"olm.gvk","value":{"group":"logging.openshift.io","version":"v1","kind":"Elasticsearch"}},{"type":"olm.gvk","value":{"group":"logging.openshift.io","version":"v1","kind":"Kibana"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"true","proxy":"true","tlsProfiles":"true","tokenAuthentication":"false"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/openshift-logging/elasticsearch-rhel9-operator@sha256:111aaa"},{"name":"elasticsearch","image":"registry.redhat.io/openshift-logging/elasticsearch-rhel9@sha256:222bbb"},{"name":"kibana","image":"registry.redhat.io/openshift-logging/kibana6-rhel9@sha256:333ccc"}]}
{"schema":"olm.bundle","name":"elasticsearch-operator.v5.8.14","package":"elasticsearch-operator","image":"registry.redhat.io/openshift-logging/elasticsearch-rhel9-operator@sha256:444ddd","replaces":"elasticsearch-operator.v5.8.10","properties":[{"type":"olm.package","value":{"packageName":"elasticsearch-operator","version":"5.8.14"}},{"type":"olm.gvk","value":{"group":"logging.openshift.io","version":"v1","kind":"Elasticsearch"}},{"type":"olm.gvk","value":{"group":"logging.openshift.io","version":"v1","kind":"Kibana"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"true","proxy":"true","tlsProfiles":"true","tokenAuthentication":"true"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/openshift-logging/elasticsearch-rhel9-operator@sha256:444ddd"},{"name":"elasticsearch","image":"registry.redhat.io/openshift-logging/elasticsearch-rhel9@sha256:555eee"},{"name":"kibana","image":"registry.redhat.io/openshift-logging/kibana6-rhel9@sha256:666fff"}]}
{"schema":"olm.bundle","name":"amqstreams.v2.7.0-0","package":"amq-streams","image":"registry.redhat.io/amq-streams/amq-streams-rhel9-operator@sha256:777aaa","properties":[{"type":"olm.package","value":{"packageName":"amq-streams","version":"2.7.0-0"}},{"type":"olm.gvk","value":{"group":"kafka.strimzi.io","version":"v1beta2","kind":"Kafka"}},{"type":"olm.gvk","value":{"group":"kafka.strimzi.io","version":"v1beta2","kind":"KafkaTopic"}},{"type":"olm.gvk","value":{"group":"kafka.strimzi.io","version":"v1beta2","kind":"KafkaConnect"}},{"type":"olm.gvk","value":{"group":"kafka.strimzi.io","version":"v1beta2","kind":"KafkaBridge"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"false","proxy":"true","tlsProfiles":"false","tokenAuthentication":"false"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/amq-streams/amq-streams-rhel9-operator@sha256:777aaa"},{"name":"kafka","image":"registry.redhat.io/amq-streams/kafka-37-rhel9@sha256:888bbb"}]}
{"schema":"olm.bundle","name":"amqstreams.v2.8.0-0","package":"amq-streams","image":"registry.redhat.io/amq-streams/amq-streams-rhel9-operator@sha256:999ccc","replaces":"amqstreams.v2.7.0-0","properties":[{"type":"olm.package","value":{"packageName":"amq-streams","version":"2.8.0-0"}},{"type":"olm.gvk","value":{"group":"kafka.strimzi.io","version":"v1beta2","kind":"Kafka"}},{"type":"olm.gvk","value":{"group":"kafka.strimzi.io","version":"v1beta2","kind":"KafkaTopic"}},{"type":"olm.gvk","value":{"group":"kafka.strimzi.io","version":"v1beta2","kind":"KafkaConnect"}},{"type":"olm.gvk","value":{"group":"kafka.strimzi.io","version":"v1beta2","kind":"KafkaBridge"}},{"type":"olm.gvk","value":{"group":"kafka.strimzi.io","version":"v1beta2","kind":"KafkaMirrorMaker2"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"true","proxy":"true","tlsProfiles":"false","tokenAuthentication":"false"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/amq-streams/amq-streams-rhel9-operator@sha256:999ccc"},{"name":"kafka","image":"registry.redhat.io/amq-streams/kafka-38-rhel9@sha256:aaabbb"}]}
{"schema":"olm.bundle","name":"openshift-pipelines-operator-rh.v1.14.4","package":"openshift-pipelines-operator-rh","image":"registry.redhat.io/openshift-pipelines/pipelines-rhel8-operator@sha256:bbbccc","properties":[{"type":"olm.package","value":{"packageName":"openshift-pipelines-operator-rh","version":"1.14.4"}},{"type":"olm.gvk","value":{"group":"operator.tekton.dev","version":"v1alpha1","kind":"TektonConfig"}},{"type":"olm.gvk","value":{"group":"operator.tekton.dev","version":"v1alpha1","kind":"TektonPipeline"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"true","proxy":"true","tlsProfiles":"true","tokenAuthentication":"true"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/openshift-pipelines/pipelines-rhel8-operator@sha256:bbbccc"},{"name":"controller","image":"registry.redhat.io/openshift-pipelines/pipelines-controller-rhel8@sha256:cccddd"}]}
{"schema":"olm.bundle","name":"openshift-pipelines-operator-rh.v1.15.1","package":"openshift-pipelines-operator-rh","image":"registry.redhat.io/openshift-pipelines/pipelines-rhel8-operator@sha256:dddeee","replaces":"openshift-pipelines-operator-rh.v1.14.4","properties":[{"type":"olm.package","value":{"packageName":"openshift-pipelines-operator-rh","version":"1.15.1"}},{"type":"olm.gvk","value":{"group":"operator.tekton.dev","version":"v1alpha1","kind":"TektonConfig"}},{"type":"olm.gvk","value":{"group":"operator.tekton.dev","version":"v1alpha1","kind":"TektonPipeline"}},{"type":"olm.gvk","value":{"group":"operator.tekton.dev","version":"v1alpha1","kind":"TektonHub"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"true","proxy":"true","tlsProfiles":"true","tokenAuthentication":"true"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/openshift-pipelines/pipelines-rhel8-operator@sha256:dddeee"},{"name":"controller","image":"registry.redhat.io/openshift-pipelines/pipelines-controller-rhel8@sha256:eeefff"}]}
{"schema":"olm.bundle","name":"servicemeshoperator.v2.5.2","package":"servicemeshoperator","image":"registry.redhat.io/openshift-service-mesh/istio-rhel9-operator@sha256:fff111","properties":[{"type":"olm.package","value":{"packageName":"servicemeshoperator","version":"2.5.2"}},{"type":"olm.gvk","value":{"group":"maistra.io","version":"v2","kind":"ServiceMeshControlPlane"}},{"type":"olm.gvk","value":{"group":"maistra.io","version":"v1","kind":"ServiceMeshMemberRoll"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"true","proxy":"true","tlsProfiles":"true","tokenAuthentication":"false"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/openshift-service-mesh/istio-rhel9-operator@sha256:fff111"},{"name":"pilot","image":"registry.redhat.io/openshift-service-mesh/pilot-rhel9@sha256:111222"},{"name":"proxyv2","image":"registry.redhat.io/openshift-service-mesh/proxyv2-rhel9@sha256:222333"}]}
{"schema":"olm.bundle","name":"servicemeshoperator.v2.6.2","package":"servicemeshoperator","image":"registry.redhat.io/openshift-service-mesh/istio-rhel9-operator@sha256:333444","replaces":"servicemeshoperator.v2.5.2","properties":[{"type":"olm.package","value":{"packageName":"servicemeshoperator","version":"2.6.2"}},{"type":"olm.gvk","value":{"group":"maistra.io","version":"v2","kind":"ServiceMeshControlPlane"}},{"type":"olm.gvk","value":{"group":"maistra.io","version":"v1","kind":"ServiceMeshMemberRoll"}},{"type":"olm.gvk","value":{"group":"maistra.io","version":"v2","kind":"ServiceMeshExtension"}},{"type":"features.operators.openshift.io","value":{"disconnected":"true","fips":"true","proxy":"true","tlsProfiles":"true","tokenAuthentication":"true"}}],"relatedImages":[{"name":"operator","image":"registry.redhat.io/openshift-service-mesh/istio-rhel9-operator@sha256:333444"},{"name":"pilot","image":"registry.redhat.io/openshift-service-mesh/pilot-rhel9@sha256:444555"},{"name":"proxyv2","image":"registry.redhat.io/openshift-service-mesh/proxyv2-rhel9@sha256:555666"}]}
`
