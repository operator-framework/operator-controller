/*
Copyright 2024.

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

package core

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// PullSecretReconciler reconciles a specific Secret object
// that contains global pull secrets for pulling Catalog images
type PullSecretReconciler struct {
	client.Client
	SecretKey    types.NamespacedName
	AuthFilePath string
}

func (r *PullSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if req.Name != r.SecretKey.Name || req.Namespace != r.SecretKey.Namespace {
		logger.Error(fmt.Errorf("received unexpected request for Secret %v/%v", req.Namespace, req.Name), "reconciliation error")
		return ctrl.Result{}, nil
	}

	secret := &corev1.Secret{}
	err := r.Get(ctx, req.NamespacedName, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("secret not found")
			return r.deleteSecretFile(logger)
		}
		logger.Error(err, "failed to get Secret")
		return ctrl.Result{}, err
	}

	return r.writeSecretToFile(logger, secret)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PullSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		WithEventFilter(newSecretPredicate(r.SecretKey)).
		Build(r)

	return err
}

func newSecretPredicate(key types.NamespacedName) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetName() == key.Name && obj.GetNamespace() == key.Namespace
	})
}

// writeSecretToFile writes the secret data to the specified file
func (r *PullSecretReconciler) writeSecretToFile(logger logr.Logger, secret *corev1.Secret) (ctrl.Result, error) {
	// image registry secrets are always stored with the key .dockerconfigjson
	// ref: https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/#registry-secret-existing-credentials
	dockerConfigJSON, ok := secret.Data[".dockerconfigjson"]
	if !ok {
		logger.Error(fmt.Errorf("expected secret.Data key not found"), "expected secret Data to contain key .dockerconfigjson")
		return ctrl.Result{}, nil
	}
	// expected format for auth.json
	// https://github.com/containers/image/blob/main/docs/containers-auth.json.5.md
	err := os.WriteFile(r.AuthFilePath, dockerConfigJSON, 0600)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to write secret data to file: %w", err)
	}
	logger.Info("saved global pull secret data locally")
	return ctrl.Result{}, nil
}

// deleteSecretFile deletes the auth file if the secret is deleted
func (r *PullSecretReconciler) deleteSecretFile(logger logr.Logger) (ctrl.Result, error) {
	logger.Info("deleting local auth file", "file", r.AuthFilePath)
	if err := os.Remove(r.AuthFilePath); err != nil {
		if os.IsNotExist(err) {
			logger.Info("auth file does not exist, nothing to delete")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to delete secret file: %w", err)
	}
	logger.Info("auth file deleted successfully")
	return ctrl.Result{}, nil
}
