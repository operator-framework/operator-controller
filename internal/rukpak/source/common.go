package source

import (
	"fmt"
)

func generateMessage(bundleName string) string {
	return fmt.Sprintf("Successfully unpacked the %s Bundle", bundleName)
}
