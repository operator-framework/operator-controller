/*
Copyright 2022.

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

package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/operator-framework/rukpak/cmd/crdvalidator/annotation"
	"github.com/operator-framework/rukpak/internal/crd"
)

// +kubebuilder:webhook:path=/validate-crd,mutating=false,failurePolicy=fail,groups="",resources=customresourcedefinitions,verbs=create;update,versions=v1,name=crd-validation-webhook.io

// CrdValidator houses a client, decoder and Handle function for ensuring
// that a CRD create/update request is safe
type CrdValidator struct {
	log     logr.Logger
	client  client.Client
	decoder *admission.Decoder
}

func NewCrdValidator(log logr.Logger, client client.Client) CrdValidator {
	return CrdValidator{
		log:    log.V(1).WithName("crdhandler"), // Default to non-verbose logs
		client: client,
	}
}

// Handle takes an incoming CRD create/update request and confirms that it is
// a safe upgrade based on the crd.Validate() function call
func (cv *CrdValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	incomingCrd := &apiextensionsv1.CustomResourceDefinition{}
	if err := cv.decoder.Decode(req, incomingCrd); err != nil {
		message := fmt.Sprintf("failed to decode CRD %q", req.Name)
		cv.log.V(0).Error(err, message)
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("%s: %v", message, err))
	}

	// Check if the request should get validated
	if disabled(incomingCrd) {
		return admission.Allowed("")
	}

	if err := crd.Validate(ctx, cv.client, incomingCrd); err != nil {
		message := fmt.Sprintf(
			"failed to validate safety of %s for CRD %q (NOTE: to disable this validation, set the %q annotation to %q): %s",
			req.Operation, req.Name, annotation.ValidationKey, annotation.Disabled, err)
		cv.log.V(0).Info(message)
		return admission.Denied(message)
	}

	cv.log.Info("admission allowed for %s of CRD %q", req.Name, req.Operation)
	return admission.Allowed("")
}

// InjectDecoder injects a decoder for the CrdValidator.
func (cv *CrdValidator) InjectDecoder(d *admission.Decoder) error {
	cv.decoder = d
	return nil
}

// disabled takes a CRD and checks its content to see crdvalidator
// is disabled explicitly
func disabled(crd *apiextensionsv1.CustomResourceDefinition) bool {
	// Check if the annotation to disable validation is set
	return crd.GetAnnotations()[annotation.ValidationKey] == annotation.Disabled
}
