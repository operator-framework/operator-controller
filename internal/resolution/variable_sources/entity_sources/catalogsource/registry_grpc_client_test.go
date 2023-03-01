package catalogsource_test

import (
	"context"
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/pkg/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity_sources/catalogsource"
)

type mockServer struct {
	packages map[string]*api.Package
	bundles  []*api.Bundle
	api.UnimplementedRegistryServer
}

func (s *mockServer) ListBundles(_ *api.ListBundlesRequest, stream api.Registry_ListBundlesServer) error {
	for _, b := range s.bundles {
		if err := stream.Send(b); err != nil {
			return err
		}
	}
	return nil
}

func (s *mockServer) GetPackage(_ context.Context, req *api.GetPackageRequest) (*api.Package, error) {
	if req != nil {
		if p, ok := s.packages[req.Name]; ok {
			return p, nil
		}
	}
	return &api.Package{}, nil
}

var _ = Describe("Registry GRPC Client", func() {
	testPackages := map[string]*api.Package{
		"prometheus": {
			Name: "prometheus",
			Channels: []*api.Channel{{
				Name:    "beta",
				CsvName: "prometheusoperator.0.47.0",
			}},
			DefaultChannelName: "beta",
		},
	}
	testBundles := []*api.Bundle{{
		CsvName:     "prometheusoperator.0.47.0",
		PackageName: "prometheus",
		ChannelName: "beta",
		BundlePath:  "quay.io/openshift-community-operators/prometheus@sha256:f0fdb1a53526bb9d3761ac6c1ae30bc73539a544efd1ce099e3d1893fadc9c6b",
		ProvidedApis: []*api.GroupVersionKind{{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "Prometheus",
			Plural:  "prometheuses",
		}, {
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "PrometheusRule",
			Plural:  "prometheusrules",
		}, {
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "Probe",
			Plural:  "probes",
		}},
		Version: "0.47.0",
		Properties: []*api.Property{
			{
				Type:  "olm.gvk",
				Value: `{"group":"monitoring.coreos.com","kind":"Prometheus","version":"v1"}`,
			}, {
				Type:  "olm.gvk",
				Value: `{"group":"monitoring.coreos.com","kind":"PrometheusRule","version":"v1"}`,
			}, {
				Type:  "olm.maxOpenShiftVersion",
				Value: `"4.8"`,
			}, {
				Type:  "olm.package",
				Value: `{"packageName":"prometheus","version":"0.47.0"}`,
			},
		},
		Replaces: "prometheusoperator.0.37.0",
	}, {
		CsvName:     "prometheusoperator.0.37.0",
		PackageName: "prometheus",
		ChannelName: "beta",
		BundlePath:  "quay.io/openshift-community-operators/prometheus@sha256:6fbd3eaa123054c5023323d1f9ab7cbea178087fcb7cb4f3e83872c6a88d39a1",
		ProvidedApis: []*api.GroupVersionKind{{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "Prometheus",
			Plural:  "prometheuses",
		}, {
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "PrometheusRule",
			Plural:  "prometheusrules",
		}},
		Version: "0.37.0",
		Properties: []*api.Property{
			{
				Type:  "olm.gvk",
				Value: `{"group":"monitoring.coreos.com","kind":"Prometheus","version":"v1"}`,
			}, {
				Type:  "olm.gvk",
				Value: `{"group":"monitoring.coreos.com","kind":"PrometheusRule","version":"v1"}`,
			}, {
				Type:  "olm.package",
				Value: `{"packageName":"prometheus","version":"0.37.0"}`,
			},
		},
	}}

	var grpcServer *grpc.Server
	done := make(chan struct{})

	BeforeEach(func() {
		lis, err := net.Listen("tcp", ":50052")
		Expect(err).To(BeNil())

		grpcServer = grpc.NewServer()
		api.RegisterRegistryServer(grpcServer, &mockServer{packages: testPackages, bundles: testBundles})

		reflection.Register(grpcServer)

		go func() {
			err = grpcServer.Serve(lis) // run till gracefulStop is called
			Expect(err).To(BeNil())
			close(done)
		}()
	})

	AfterEach(func() {
		grpcServer.GracefulStop()

		<-done
	})

	It("lists entities from a grpc registry server", func() {
		entities, err := catalogsource.NewRegistryGRPCClient(1*time.Minute).ListEntities(context.TODO(), &v1alpha1.CatalogSource{
			Spec: v1alpha1.CatalogSourceSpec{
				Address: ":50052",
			},
		})
		Expect(err).To(BeNil())

		Expect(entities).To(ConsistOf([]*input.Entity{{
			ID: "//prometheus/beta/0.47.0",
			Properties: map[string]string{
				"olm.gvk":                    `[{"group":"monitoring.coreos.com","kind":"Probe","version":"v1"},{"group":"monitoring.coreos.com","kind":"Prometheus","version":"v1"},{"group":"monitoring.coreos.com","kind":"PrometheusRule","version":"v1"}]`,
				"olm.maxOpenShiftVersion":    `["4.8"]`,
				"olm.package":                `{"packageName":"prometheus","version":"0.47.0"}`,
				"olm.channel":                `{"channelName":"beta","priority":0,"replaces":"prometheusoperator.0.37.0"}`,
				"olm.package.defaultChannel": "beta",
				"olm.bundle.path":            "quay.io/openshift-community-operators/prometheus@sha256:f0fdb1a53526bb9d3761ac6c1ae30bc73539a544efd1ce099e3d1893fadc9c6b",
			},
		}, {
			ID: "//prometheus/beta/0.37.0",
			Properties: map[string]string{
				"olm.gvk":                    `[{"group":"monitoring.coreos.com","kind":"Prometheus","version":"v1"},{"group":"monitoring.coreos.com","kind":"PrometheusRule","version":"v1"}]`,
				"olm.package":                `{"packageName":"prometheus","version":"0.37.0"}`,
				"olm.channel":                `{"channelName":"beta","priority":0}`,
				"olm.package.defaultChannel": "beta",
				"olm.bundle.path":            "quay.io/openshift-community-operators/prometheus@sha256:6fbd3eaa123054c5023323d1f9ab7cbea178087fcb7cb4f3e83872c6a88d39a1",
			},
		}}))
	})
})
