package catalogsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	catalogsourceapi "github.com/operator-framework/operator-registry/pkg/api"
	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

type RegistryClient interface {
	ListEntities(ctx context.Context, catsrc *v1alpha1.CatalogSource) ([]*input.Entity, error)
}

type registryGRPCClient struct {
	timeout time.Duration
}

func NewRegistryGRPCClient(grpcTimeout time.Duration) RegistryClient {
	if grpcTimeout == 0 {
		grpcTimeout = DefaultGRPCTimeout
	}
	return &registryGRPCClient{timeout: grpcTimeout}
}

func (r *registryGRPCClient) ListEntities(ctx context.Context, catalogSource *v1alpha1.CatalogSource) ([]*input.Entity, error) {
	// TODO: create GRPC connections separately
	conn, err := ConnectGRPCWithTimeout(ctx, catalogSource.Address(), r.timeout)
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return nil, err
	}

	catsrcClient := catalogsourceapi.NewRegistryClient(conn)
	stream, err := catsrcClient.ListBundles(ctx, &catalogsourceapi.ListBundlesRequest{})

	if err != nil {
		return nil, fmt.Errorf("ListBundles failed: %v", err)
	}

	var entities []*input.Entity
	catalogPackages := map[string]*catalogsourceapi.Package{}
	catalogSourceID := fmt.Sprintf("%s/%s", catalogSource.Namespace, catalogSource.Name)
	for {
		bundle, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return entities, fmt.Errorf("failed to read bundle stream: %v", err)
		}

		packageKey := fmt.Sprintf("%s/%s", catalogSourceID, bundle.PackageName)
		pkg, ok := catalogPackages[packageKey]
		if !ok {
			pkg, err = catsrcClient.GetPackage(ctx, &catalogsourceapi.GetPackageRequest{Name: bundle.PackageName})
			if err != nil {
				return entities, fmt.Errorf("failed to get package %s: %v", bundle.PackageName, err)
			}
			catalogPackages[packageKey] = pkg
		}

		entity, err := EntityFromBundle(catalogSourceID, pkg, bundle)
		if err != nil {
			return entities, fmt.Errorf("failed to parse entity %s: %v", entity.Identifier(), err)
		}
		entities = append(entities, entity)
	}
	return entities, nil
}

const DefaultGRPCTimeout = 2 * time.Minute

func ConnectGRPCWithTimeout(ctx context.Context, address string, timeout time.Duration) (conn *grpc.ClientConn, err error) {
	conn, err = grpcConnection(address)
	if err != nil {
		return nil, fmt.Errorf("GRPC connection failed: %v", err)
	}

	if timeout == 0 {
		timeout = DefaultGRPCTimeout
	}

	if err := waitForGRPCWithTimeout(ctx, conn, timeout, address); err != nil {
		return conn, fmt.Errorf("GRPC timeout: %v", err)
	}

	return conn, nil
}

func waitForGRPCWithTimeout(ctx context.Context, conn *grpc.ClientConn, timeout time.Duration, address string) error {
	if conn == nil {
		return fmt.Errorf("nil connection")
	}
	state := conn.GetState()
	if state == connectivity.Ready {
		return nil
	}
	oldState := state
	fmt.Printf("%v %s Connection state: %v\n", time.Now(), address, state)
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		select {
		case <-ctx2.Done():
			return fmt.Errorf("%v %s timed out waiting for ready state, %v", time.Now(), address, timeout)
		default:
			state := conn.GetState()
			if state != oldState {
				fmt.Printf("%v %s Connection state: %v\n", time.Now(), address, state)
				oldState = state
			}
			if state == connectivity.Ready {
				return nil
			}
		}
	}
}

func grpcConnection(address string) (*grpc.ClientConn, error) {
	dialOptions := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	proxyURL, err := grpcProxyURL(address)
	if err != nil {
		return nil, err
	}

	if proxyURL != nil {
		dialOptions = append(dialOptions, grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			dialer, err := proxy.FromURL(proxyURL, &net.Dialer{})
			if err != nil {
				return nil, err
			}
			return dialer.Dial("tcp", addr)
		}))
	}

	return grpc.Dial(address, dialOptions...)
}

func grpcProxyURL(addr string) (*url.URL, error) {
	// Handle ip addresses
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	url, err := url.Parse(host)
	if err != nil {
		return nil, err
	}

	// Hardcode fields required for proxy resolution
	url.Host = addr
	url.Scheme = "http"

	// Override HTTPS_PROXY and HTTP_PROXY with GRPC_PROXY
	proxyConfig := &httpproxy.Config{
		HTTPProxy:  getGRPCProxyEnv(),
		HTTPSProxy: getGRPCProxyEnv(),
		NoProxy:    getEnvAny("NO_PROXY", "no_proxy"),
		CGI:        os.Getenv("REQUEST_METHOD") != "",
	}

	// Check if a proxy should be used based on environment variables
	return proxyConfig.ProxyFunc()(url)
}

func getGRPCProxyEnv() string {
	return getEnvAny("GRPC_PROXY", "grpc_proxy")
}

func getEnvAny(names ...string) string {
	for _, n := range names {
		if val := os.Getenv(n); val != "" {
			return val
		}
	}
	return ""
}
