/*
Copyright 2023.

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

package controllers_test

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/test"
)

func newScheme(t *testing.T) *apimachineryruntime.Scheme {
	sch := apimachineryruntime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(sch))
	return sch
}

var config *rest.Config

func TestMain(m *testing.M) {
	testEnv := test.NewEnv()

	var err error
	config, err = testEnv.Start()
	utilruntime.Must(err)
	if config == nil {
		log.Panic("expected cfg to not be nil")
	}

	code := m.Run()
	// Use Eventually wrapper for graceful test environment teardown
	// controller-runtime v0.23.0+ requires this to prevent timing-related errors
	stopErr := test.StopWithRetry(testEnv, time.Minute, time.Second)
	utilruntime.Must(stopErr)
	os.Exit(code)
}
