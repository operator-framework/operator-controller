/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateConfigWebhookTLS(t *testing.T) {
	t.Run("defaults to the catalog server certificate", func(t *testing.T) {
		config := &config{
			certFile: "/var/certs/tls.crt",
			keyFile:  "/var/certs/tls.key",
		}

		require.NoError(t, validateConfig(config))
		require.Equal(t, config.certFile, config.webhookCertFile)
		require.Equal(t, config.keyFile, config.webhookKeyFile)
	})

	t.Run("accepts a separate webhook certificate", func(t *testing.T) {
		config := &config{
			certFile:        "/var/certs/tls.crt",
			keyFile:         "/var/certs/tls.key",
			webhookCertFile: "/var/webhook-certs/tls.crt",
			webhookKeyFile:  "/var/webhook-certs/tls.key",
		}

		require.NoError(t, validateConfig(config))
	})

	t.Run("requires both webhook certificate files", func(t *testing.T) {
		config := &config{webhookCertFile: "/var/webhook-certs/tls.crt"}

		require.EqualError(t, validateConfig(config), "webhook-tls-cert and webhook-tls-key flags must be used together")
	})
}
