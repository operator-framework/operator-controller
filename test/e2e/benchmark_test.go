package e2e

import (
	"context"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"
)

func BenchmarkCreateClusterCatalog(b *testing.B) {
	catalogImageRef := os.Getenv(testCatalogRefEnvVar)
	if catalogImageRef == "" {
		b.Fatalf("environment variable %s is not set", testCatalogRefEnvVar)
	}
	ctx := context.Background()
	b.ResetTimer()
	// b.RunParallel(func(pb *testing.PB) {
	// 	for pb.Next() {
	// 		catalogObj, err := createTestCatalog(ctx, getRandomStringParallel(6), catalogImageRef)
	// 		if err != nil {
	// 			b.Logf("failed to create ClusterCatalog: %v", err)
	// 		}

	// 		if err := deleteTestCatalog(ctx, catalogObj); err != nil {
	// 			b.Logf("failed to remove ClusterCatalog: %v", err)
	// 		}
	// 	}
	// })
	for i := 0; i < b.N; i++ {
		catalogObj, err := createTestCatalog(ctx, getRandomString(8), catalogImageRef)
		if err != nil {
			b.Logf("failed to create ClusterCatalog: %v", err)
		}

		if err := deleteTestCatalog(ctx, catalogObj); err != nil {
			b.Logf("failed to remove ClusterCatalog: %v", err)
		}
	}
}

var (
	mu        sync.Mutex
	usedChars = make(map[string]struct{})
	alphabet  = "abcdefghijklmnopqrstuvwxyz"
)

func getRandomStringParallel(length int) string {
	// Ensure we seed the random number generator only once
	rand.Seed(time.Now().UnixNano())

	// Lock to ensure no concurrent access to shared resources (e.g., usedChars)
	mu.Lock()
	defer mu.Unlock()

	// Try generating a random string and ensure it's unique
	for {
		var result []rune
		for i := 0; i < length; i++ {
			result = append(result, rune(alphabet[rand.Intn(len(alphabet))]))
		}
		// Convert result to string
		randomStr := string(result)

		// Check if the generated string is unique
		if _, exists := usedChars[randomStr]; !exists {
			// If it's unique, add it to the map and return it
			usedChars[randomStr] = struct{}{}
			return randomStr
		}
	}
}

// GetRandomString generates a random string of the given length
func getRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz"
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}
