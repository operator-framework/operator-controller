package e2e

import (
	"context"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/util/rand"
)

func BenchmarkCreateClusterCatalog(b *testing.B) {
	catalogImageRef := os.Getenv(testCatalogRefEnvVar)
	if catalogImageRef == "" {
		b.Fatalf("environment variable %s is not set", testCatalogRefEnvVar)
	}
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			catalogObj, err := createTestCatalog(ctx, rand.String(6), catalogImageRef)
			if err != nil {
				b.Logf("failed to create ClusterCatalog: %v", err)
			}

			if err := deleteTestCatalog(ctx, catalogObj); err != nil {
				b.Logf("failed to remove ClusterCatalog: %v", err)
			}
		}
	})
	// for i := 0; i < b.N; i++ {
	// 	catalogObj, err := createTestCatalog(ctx, rand.String(8), catalogImageRef)
	// 	if err != nil {
	// 		b.Logf("failed to create ClusterCatalog: %v", err)
	// 	}

	// 	if err := deleteTestCatalog(ctx, catalogObj); err != nil {
	// 		b.Logf("failed to remove ClusterCatalog: %v", err)
	// 	}
	// }
}
