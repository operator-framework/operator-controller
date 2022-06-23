package testutil

import "k8s.io/apimachinery/pkg/util/rand"

func GenName(namePrefix string) string {
	return namePrefix + rand.String(5)
}
