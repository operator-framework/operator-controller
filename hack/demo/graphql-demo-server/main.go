package main

import (
	"fmt"
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

func main() {
	addr := ":9376"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}

	store := &demoCatalogStore{
		catalogs: map[string]fs.FS{
			"example-catalog": buildCatalog(),
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
	fmt.Fprintf(os.Stderr, "Endpoint: POST http://localhost%s/catalogs/example-catalog/api/v1/graphql\n", addr)
	fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop.\n")

	go func() {
		// nolint:gosec
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

func buildCatalog() fs.FS {
	return fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(catalogJSON),
		},
	}
}

// catalogJSON contains sample FBC data for demonstration purposes.
// Each line is a separate JSON object (JSONL format parsed by WalkMetasFS).
const catalogJSON = `{"schema":"olm.package","name":"database-operator","defaultChannel":"stable","description":"An operator for managing database instances."}
{"schema":"olm.package","name":"logging-operator","defaultChannel":"stable","description":"Logging operator for collecting and forwarding application logs."}
{"schema":"olm.package","name":"messaging-operator","defaultChannel":"stable","description":"Messaging broker operator based on Apache Kafka."}
{"schema":"olm.package","name":"cicd-operator","defaultChannel":"latest","description":"CI/CD pipeline operator for cloud-native workflows."}
{"schema":"olm.package","name":"mesh-operator","defaultChannel":"stable","description":"Service mesh operator for microservices networking."}
{"schema":"olm.channel","name":"stable","package":"database-operator","entries":[{"name":"database-operator.v1.6.0","skipRange":">=0.1.17 <1.6.0"},{"name":"database-operator.v1.7.0","replaces":"database-operator.v1.6.0","skipRange":">=0.1.17 <1.7.0"},{"name":"database-operator.v1.7.1","replaces":"database-operator.v1.7.0","skipRange":">=1.0.0 <1.7.1"}]}
{"schema":"olm.channel","name":"stable","package":"logging-operator","entries":[{"name":"logging-operator.v5.8.10","skipRange":">=5.6.0 <5.8.10"},{"name":"logging-operator.v5.8.14","replaces":"logging-operator.v5.8.10","skipRange":">=5.6.0 <5.8.14"}]}
{"schema":"olm.channel","name":"stable","package":"messaging-operator","entries":[{"name":"messaging-operator.v2.7.0","skipRange":">=2.0.0 <2.7.0"},{"name":"messaging-operator.v2.8.0","replaces":"messaging-operator.v2.7.0","skipRange":">=2.0.0 <2.8.0"}]}
{"schema":"olm.channel","name":"latest","package":"cicd-operator","entries":[{"name":"cicd-operator.v1.14.4","skipRange":">=1.5.0 <1.14.4"},{"name":"cicd-operator.v1.15.1","replaces":"cicd-operator.v1.14.4","skipRange":">=1.5.0 <1.15.1"}]}
{"schema":"olm.channel","name":"stable","package":"mesh-operator","entries":[{"name":"mesh-operator.v2.5.2","skipRange":">=2.0.0 <2.5.2"},{"name":"mesh-operator.v2.6.2","replaces":"mesh-operator.v2.5.2","skipRange":">=2.0.0 <2.6.2"}]}
{"schema":"olm.bundle","name":"database-operator.v1.6.0","package":"database-operator","image":"quay.io/example/database-operator@sha256:aaa111","properties":[{"type":"olm.package","value":{"packageName":"database-operator","version":"1.6.0"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"Database"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"DatabaseBackup"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"DatabaseRestore"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/database-operator@sha256:aaa111"},{"name":"database","image":"quay.io/example/database@sha256:bbb222"}]}
{"schema":"olm.bundle","name":"database-operator.v1.7.0","package":"database-operator","image":"quay.io/example/database-operator@sha256:ccc333","replaces":"database-operator.v1.6.0","properties":[{"type":"olm.package","value":{"packageName":"database-operator","version":"1.7.0"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"Database"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"DatabaseBackup"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"DatabaseRestore"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"DatabaseSnapshot"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/database-operator@sha256:ccc333"},{"name":"database","image":"quay.io/example/database@sha256:ddd444"}]}
{"schema":"olm.bundle","name":"database-operator.v1.7.1","package":"database-operator","image":"quay.io/example/database-operator@sha256:eee555","replaces":"database-operator.v1.7.0","properties":[{"type":"olm.package","value":{"packageName":"database-operator","version":"1.7.1"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"Database"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"DatabaseBackup"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"DatabaseRestore"}},{"type":"olm.gvk","value":{"group":"db.example.io","version":"v1alpha1","kind":"DatabaseSnapshot"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/database-operator@sha256:eee555"},{"name":"database","image":"quay.io/example/database@sha256:fff666"}]}
{"schema":"olm.bundle","name":"logging-operator.v5.8.10","package":"logging-operator","image":"quay.io/example/logging-operator@sha256:111aaa","properties":[{"type":"olm.package","value":{"packageName":"logging-operator","version":"5.8.10"}},{"type":"olm.gvk","value":{"group":"logging.example.io","version":"v1","kind":"LogCollector"}},{"type":"olm.gvk","value":{"group":"logging.example.io","version":"v1","kind":"LogForwarder"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/logging-operator@sha256:111aaa"},{"name":"collector","image":"quay.io/example/log-collector@sha256:222bbb"},{"name":"forwarder","image":"quay.io/example/log-forwarder@sha256:333ccc"}]}
{"schema":"olm.bundle","name":"logging-operator.v5.8.14","package":"logging-operator","image":"quay.io/example/logging-operator@sha256:444ddd","replaces":"logging-operator.v5.8.10","properties":[{"type":"olm.package","value":{"packageName":"logging-operator","version":"5.8.14"}},{"type":"olm.gvk","value":{"group":"logging.example.io","version":"v1","kind":"LogCollector"}},{"type":"olm.gvk","value":{"group":"logging.example.io","version":"v1","kind":"LogForwarder"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/logging-operator@sha256:444ddd"},{"name":"collector","image":"quay.io/example/log-collector@sha256:555eee"},{"name":"forwarder","image":"quay.io/example/log-forwarder@sha256:666fff"}]}
{"schema":"olm.bundle","name":"messaging-operator.v2.7.0","package":"messaging-operator","image":"quay.io/example/messaging-operator@sha256:777aaa","properties":[{"type":"olm.package","value":{"packageName":"messaging-operator","version":"2.7.0"}},{"type":"olm.gvk","value":{"group":"kafka.example.io","version":"v1beta2","kind":"Kafka"}},{"type":"olm.gvk","value":{"group":"kafka.example.io","version":"v1beta2","kind":"KafkaTopic"}},{"type":"olm.gvk","value":{"group":"kafka.example.io","version":"v1beta2","kind":"KafkaConnect"}},{"type":"olm.gvk","value":{"group":"kafka.example.io","version":"v1beta2","kind":"KafkaBridge"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/messaging-operator@sha256:777aaa"},{"name":"kafka","image":"quay.io/example/kafka@sha256:888bbb"}]}
{"schema":"olm.bundle","name":"messaging-operator.v2.8.0","package":"messaging-operator","image":"quay.io/example/messaging-operator@sha256:999ccc","replaces":"messaging-operator.v2.7.0","properties":[{"type":"olm.package","value":{"packageName":"messaging-operator","version":"2.8.0"}},{"type":"olm.gvk","value":{"group":"kafka.example.io","version":"v1beta2","kind":"Kafka"}},{"type":"olm.gvk","value":{"group":"kafka.example.io","version":"v1beta2","kind":"KafkaTopic"}},{"type":"olm.gvk","value":{"group":"kafka.example.io","version":"v1beta2","kind":"KafkaConnect"}},{"type":"olm.gvk","value":{"group":"kafka.example.io","version":"v1beta2","kind":"KafkaBridge"}},{"type":"olm.gvk","value":{"group":"kafka.example.io","version":"v1beta2","kind":"KafkaMirrorMaker"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/messaging-operator@sha256:999ccc"},{"name":"kafka","image":"quay.io/example/kafka@sha256:aaabbb"}]}
{"schema":"olm.bundle","name":"cicd-operator.v1.14.4","package":"cicd-operator","image":"quay.io/example/cicd-operator@sha256:bbbccc","properties":[{"type":"olm.package","value":{"packageName":"cicd-operator","version":"1.14.4"}},{"type":"olm.gvk","value":{"group":"tekton.example.io","version":"v1alpha1","kind":"Pipeline"}},{"type":"olm.gvk","value":{"group":"tekton.example.io","version":"v1alpha1","kind":"PipelineRun"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/cicd-operator@sha256:bbbccc"},{"name":"controller","image":"quay.io/example/pipeline-controller@sha256:cccddd"}]}
{"schema":"olm.bundle","name":"cicd-operator.v1.15.1","package":"cicd-operator","image":"quay.io/example/cicd-operator@sha256:dddeee","replaces":"cicd-operator.v1.14.4","properties":[{"type":"olm.package","value":{"packageName":"cicd-operator","version":"1.15.1"}},{"type":"olm.gvk","value":{"group":"tekton.example.io","version":"v1alpha1","kind":"Pipeline"}},{"type":"olm.gvk","value":{"group":"tekton.example.io","version":"v1alpha1","kind":"PipelineRun"}},{"type":"olm.gvk","value":{"group":"tekton.example.io","version":"v1alpha1","kind":"Task"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/cicd-operator@sha256:dddeee"},{"name":"controller","image":"quay.io/example/pipeline-controller@sha256:eeefff"}]}
{"schema":"olm.bundle","name":"mesh-operator.v2.5.2","package":"mesh-operator","image":"quay.io/example/mesh-operator@sha256:fff111","properties":[{"type":"olm.package","value":{"packageName":"mesh-operator","version":"2.5.2"}},{"type":"olm.gvk","value":{"group":"mesh.example.io","version":"v1","kind":"ServiceMesh"}},{"type":"olm.gvk","value":{"group":"mesh.example.io","version":"v1","kind":"Gateway"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/mesh-operator@sha256:fff111"},{"name":"proxy","image":"quay.io/example/mesh-proxy@sha256:111222"},{"name":"sidecar","image":"quay.io/example/mesh-sidecar@sha256:222333"}]}
{"schema":"olm.bundle","name":"mesh-operator.v2.6.2","package":"mesh-operator","image":"quay.io/example/mesh-operator@sha256:333444","replaces":"mesh-operator.v2.5.2","properties":[{"type":"olm.package","value":{"packageName":"mesh-operator","version":"2.6.2"}},{"type":"olm.gvk","value":{"group":"mesh.example.io","version":"v1","kind":"ServiceMesh"}},{"type":"olm.gvk","value":{"group":"mesh.example.io","version":"v1","kind":"Gateway"}},{"type":"olm.gvk","value":{"group":"mesh.example.io","version":"v1","kind":"VirtualService"}}],"relatedImages":[{"name":"operator","image":"quay.io/example/mesh-operator@sha256:333444"},{"name":"proxy","image":"quay.io/example/mesh-proxy@sha256:444555"},{"name":"sidecar","image":"quay.io/example/mesh-sidecar@sha256:555666"}]}
`
