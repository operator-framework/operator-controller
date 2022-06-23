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

package v1alpha1

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var bundlelog = logf.Log.WithName("bundle-resource")

func (b *Bundle) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(b).
		Complete()
}

//+kubebuilder:webhook:path=/validate-core-rukpak-io-v1alpha1-bundle,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.rukpak.io,resources=bundles,verbs=create;update,versions=v1alpha1,name=vbundles.core.rukpak.io,admissionReviewVersions=v1

var _ webhook.Validator = &Bundle{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (b *Bundle) ValidateCreate() error {
	bundlelog.V(1).Info("validate create", "name", b.Name)

	return checkBundleSource(b)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (b *Bundle) ValidateUpdate(old runtime.Object) error {
	bundlelog.V(1).Info("validate update", "name", b.Name)

	oldBundle := old.(*Bundle)
	if err := checkImmutableSpec(oldBundle, b); err != nil {
		return err
	}

	return checkBundleSource(b)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (b *Bundle) ValidateDelete() error {
	bundlelog.V(1).Info("validate delete", "name", b.Name)

	return nil
}

func checkBundleSource(r *Bundle) error {
	switch typ := r.Spec.Source.Type; typ {
	case SourceTypeImage:
		if r.Spec.Source.Image == nil {
			return fmt.Errorf("bundle.spec.source.image must be set for source type \"image\"")
		}
	case SourceTypeGit:
		if r.Spec.Source.Git == nil {
			return fmt.Errorf("bundle.spec.source.git must be set for source type \"git\"")
		}
		if strings.HasPrefix(filepath.Clean(r.Spec.Source.Git.Directory), "../") {
			return fmt.Errorf(`bundle.spec.source.git.directory begins with "../": directory must define path within the repository`)
		}
	}
	return nil
}

func checkImmutableSpec(oldBundle, newBundle *Bundle) error {
	if !equality.Semantic.DeepEqual(oldBundle.Spec, newBundle.Spec) {
		return errors.New("bundle.spec is immutable")
	}
	return nil
}
