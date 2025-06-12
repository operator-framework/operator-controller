/*
Copyright 2025.

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

package sa

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	// taken from a kind run
	goodSa = "eyJhbGciOiJSUzI1NiIsImtpZCI6IjdyM3VIbkJ0VlRQVy1uWWlsSVFCV2pfQmdTS0RIdjZHNDBVT1hDSVFtZmcifQ.eyJhdWQiOlsiaHR0cHM6Ly9rdWJlcm5ldGVzLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWwiXSwiZXhwIjoxNzgwNTEwMjAwLCJpYXQiOjE3NDg5NzQyMDAsImlzcyI6Imh0dHBzOi8va3ViZXJuZXRlcy5kZWZhdWx0LnN2Yy5jbHVzdGVyLmxvY2FsIiwianRpIjoiNTQ2OThmZGYtNzg4NC00YzhkLWI5NzctYTg4YThiYmY3ODQxIiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJvbG12MS1zeXN0ZW0iLCJub2RlIjp7Im5hbWUiOiJvcGVyYXRvci1jb250cm9sbGVyLWUyZS1jb250cm9sLXBsYW5lIiwidWlkIjoiZWY0YjdkNGQtZmUxZi00MThkLWIyZDAtM2ZmYWJmMWQ0ZDI3In0sInBvZCI6eyJuYW1lIjoib3BlcmF0b3ItY29udHJvbGxlci1jb250cm9sbGVyLW1hbmFnZXItNjU3Njg1ZGNkYy01cTZ0dCIsInVpZCI6IjE4MmFkNTkxLWUzYTktNDMyNC1hMjk4LTg0NzIxY2Q0OTAzYSJ9LCJzZXJ2aWNlYWNjb3VudCI6eyJuYW1lIjoib3BlcmF0b3ItY29udHJvbGxlci1jb250cm9sbGVyLW1hbmFnZXIiLCJ1aWQiOiI3MDliZTA4OS00OTI1LTQ2NjYtYjA1Ny1iYWMyNmVmYWJjMGIifSwid2FybmFmdGVyIjoxNzQ4OTc3ODA3fSwibmJmIjoxNzQ4OTc0MjAwLCJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnQ6b2xtdjEtc3lzdGVtOm9wZXJhdG9yLWNvbnRyb2xsZXItY29udHJvbGxlci1tYW5hZ2VyIn0.OjExhuNHdMZjdGwDXM0bWQnJKcfLNpEJ2S47BzlAa560uNw8EwMItlfpG970umQBbVPWhyhUBFimUD5XmXWAlrNvhFwpOLXw2W978Obs1mna5JWcHliC6IkwrOMCh5k9XReQ9-KBdw36QY1G2om77-7mNtPNPg9lg5TQaLuNGrIhX9EC_tucbflXSvB-SA243J_X004W4HkJirt6vVH5FoRg-MDohXm0C4bhTeaXfOtTW6fwsnpomCKso7apu_eOG9E2h8CXXYKhZg4Jrank_Ata8J1lANh06FuxRQK-vwqFrW3_9rscGxweM5CbeicZFOc6MDIuYtgR515YTHPbUA" // notsecret
	badSa1 = "eyJhbGciOiJSUzI1NiIsImtpZCI6IjdyM3VIbkJ0VlRQVy1uWWlsSVFCV2pfQmdTS0RIdjZHNDBVT1hDSVFtZmcifQ.eyJhdWQiOlsiaHR0cHM6Ly9rdWJlcm5ldGVzLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWwiXSwiZXhwIjoxNzgwNTEwMjAwLCJpYXQiOjE3NDg5NzQyMDAsImlzcyI6Imh0dHBzOi8va3ViZXJuZXRlcy5kZWZhdWx0LnN2Yy5jbHVzdGVyLmxvY2FsIiwianRpIjoiNTQ2OThmZGYtNzg4NC00YzhkLWI5NzctYTg4YThiYmY3ODQxIiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJvbG12MS1zeXN0ZW0iLCJub2RlIjp7Im5hbWUiOiJvcGVyYXRvci1jb250cm9sbGVyLWUyZS1jb250cm9sLXBsYW5lIiwidWlkIjoiZWY0YjdkNGQtZmUxZi00MThkLWIyZDAtM2ZmYWJmMWQ0ZDI3In0sInBvZCI6eyJuYW1lIjoib3BlcmF0b3ItY29udHJvbGxlci1jb250cm9sbGVyLW1hbmFnZXItNjU3Njg1ZGNkYy01cTZ0dCIsInVpZCI6IjE4MmFkNTkxLWUzYTktNDMyNC1hMjk4LTg0NzIxY2Q0OTAzYSJ9LCJzZXJ2aWNlYWNjb3VudCI6eyJuYW1lIjoib3BlcmF0b3ItY29udHJvbGxlci1jb250cm9sbGVyLW1hbmFnZXIiLCJ1aWQiOiI3MDliZTA4OS00OTI1LTQ2NjYtYjA1Ny1iYWMyNmVmYWJjMGIifSwid2FybmFmdGVyIjoxNzQ4OTc3ODA3fSwibmJmIjoxNzQ4OTc0MjAwLCJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnRzOm9sbXYxLXN5c3RlbSJ9.OjExhuNHdMZjdGwDXM0bWQnJKcfLNpEJ2S47BzlAa560uNw8EwMItlfpG970umQBbVPWhyhUBFimUD5XmXWAlrNvhFwpOLXw2W978Obs1mna5JWcHliC6IkwrOMCh5k9XReQ9-KBdw36QY1G2om77-7mNtPNPg9lg5TQaLuNGrIhX9EC_tucbflXSvB-SA243J_X004W4HkJirt6vVH5FoRg-MDohXm0C4bhTeaXfOtTW6fwsnpomCKso7apu_eOG9E2h8CXXYKhZg4Jrank_Ata8J1lANh06FuxRQK-vwqFrW3_9rscGxweM5CbeicZFOc6MDIuYtgR515YTHPbUA"                                                    // notsecret
	badSa2 = "eyJhbGciOiJSUzI1NiIsImtpZCI6IjdyM3VIbkJ0VlRQVy1uWWlsSVFCV2pfQmdTS0RIdjZHNDBVT1hDSVFtZmcifQ"                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            // notsecret
)

func TestGetServiceAccount(t *testing.T) {
	nn, err := getServiceAccountInternal([]byte(goodSa), nil)
	require.NoError(t, err)
	require.Equal(t, "olmv1-system", nn.Namespace)
	require.Equal(t, "operator-controller-controller-manager", nn.Name)

	_, err = getServiceAccountInternal([]byte{}, fmt.Errorf("this is a test error"))
	require.ErrorContains(t, err, "this is a test")

	// Modified the subject to be invalid
	_, err = getServiceAccountInternal([]byte(badSa1), nil)
	require.ErrorContains(t, err, "badly formatted subject")

	// Only includes a header
	_, err = getServiceAccountInternal([]byte(badSa2), nil)
	require.ErrorContains(t, err, "token is malformed")
}
