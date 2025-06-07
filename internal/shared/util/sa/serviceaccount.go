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
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// Returns nameaspce/serviceaccount name
func GetServiceAccount() (k8stypes.NamespacedName, error) {
	return getServiceAccountInternal(os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token"))
}

func getServiceAccountInternal(data []byte, err error) (k8stypes.NamespacedName, error) {
	if err != nil {
		return k8stypes.NamespacedName{}, err
	}
	// Not verifying the token, we just want to extract the subject
	token, _, err := jwt.NewParser([]jwt.ParserOption{}...).ParseUnverified(string(data), jwt.MapClaims{})
	if err != nil {
		return k8stypes.NamespacedName{}, err
	}
	subject, err := token.Claims.GetSubject()
	if err != nil {
		return k8stypes.NamespacedName{}, err
	}
	subjects := strings.Split(subject, ":")
	if len(subjects) != 4 {
		return k8stypes.NamespacedName{}, fmt.Errorf("badly formatted subject: %s", subject)
	}
	return k8stypes.NamespacedName{Namespace: subjects[2], Name: subjects[3]}, nil
}
