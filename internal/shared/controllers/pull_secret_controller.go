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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/google/renameio/v2"
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
	SecretKey                 *types.NamespacedName
	ServiceAccountKey         types.NamespacedName
	ServiceAccountPullSecrets []types.NamespacedName
	AuthFilePath              string
}

func (r *PullSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("pull-secret-reconciler")

	logger.Info("processing event", logName(req.NamespacedName)...)
	defer logger.Info("processed event", logName(req.NamespacedName)...)

	secrets := []*corev1.Secret{}

	if r.SecretKey != nil { //nolint:nestif
		secret, err := r.getSecret(ctx, logger, *r.SecretKey)
		if err != nil {
			return ctrl.Result{}, err
		}
		// Add the configured pull secret to the list of secrets
		if secret != nil {
			secrets = append(secrets, secret)
		}
	}

	// Grab all the pull secrets from the serviceaccount and add them to the list of secrets
	sa := &corev1.ServiceAccount{}
	logger.Info("serviceaccount", "name", r.ServiceAccountKey)
	if err := r.Get(ctx, r.ServiceAccountKey, sa); err != nil { //nolint:nestif
		if apierrors.IsNotFound(err) {
			logger.Info("serviceaccount not found", logName(r.ServiceAccountKey)...)
		} else {
			logger.Error(err, "failed to get serviceaccount", logName(r.ServiceAccountKey)...)
			return ctrl.Result{}, err
		}
	} else {
		nn := types.NamespacedName{Namespace: r.ServiceAccountKey.Namespace}
		pullSecrets := []types.NamespacedName{}
		for _, ips := range sa.ImagePullSecrets {
			nn.Name = ips.Name
			// This is to update the list of secrets that we are filtering on
			// Add all secrets regardless if they exist or not
			pullSecrets = append(pullSecrets, nn)

			secret, err := r.getSecret(ctx, logger, nn)
			if err != nil {
				return ctrl.Result{}, err
			}
			if secret != nil {
				secrets = append(secrets, secret)
			}
		}
		// update list of pull secrets from service account
		logger.Info("updating list of pull secrets", "list", pullSecrets)
		r.ServiceAccountPullSecrets = pullSecrets
	}

	if len(secrets) == 0 {
		return ctrl.Result{}, r.deleteSecretFile(logger)
	}
	return ctrl.Result{}, r.writeSecretToFile(logger, secrets)
}

func (r *PullSecretReconciler) getSecret(ctx context.Context, logger logr.Logger, nn types.NamespacedName) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, *r.SecretKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("secret not found", logName(nn)...)
			return nil, nil
		}
		logger.Error(err, "failed to get secret", logName(nn)...)
		return nil, err
	}
	logger.Info("found secret", logName(nn)...)
	return secret, nil
}

// Helper function to log NamespacedNames
func logName(nn types.NamespacedName) []any {
	return []any{"name", nn.Name, "namespace", nn.Namespace}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PullSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		Named("pull-secret-controller").
		WithEventFilter(newSecretPredicate(r)).
		Build(r)
	if err != nil {
		return err
	}

	_, err = ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ServiceAccount{}).
		Named("service-account-controller").
		WithEventFilter(newNamespacedPredicate(r.ServiceAccountKey)).
		Build(r)

	return err
}

// Filters based on the global SecretKey, or any pull secret from the serviceaccount
func newSecretPredicate(r *PullSecretReconciler) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		nn := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		if r.SecretKey != nil && nn == *r.SecretKey {
			return true
		}
		for _, ps := range r.ServiceAccountPullSecrets {
			if nn == ps {
				return true
			}
		}
		return false
	})
}

func newNamespacedPredicate(key types.NamespacedName) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetName() == key.Name && obj.GetNamespace() == key.Namespace
	})
}

// Golang representation of the docker configuration - either dockerconfigjson or dockercfg formats.
// This allows us to merge the two formats together, regardless of type, and dump it out as a
// dockerconfigjson for use my contaners/images
type dockerConfigJSON struct {
	Auths dockerCfg `json:"auths"`
}

type dockerCfg map[string]authEntries

type authEntries struct {
	Auth  string `json:"auth"`
	Email string `json:"email,omitempty"`
}

// writeSecretToFile writes the secret data to the specified file
func (r *PullSecretReconciler) writeSecretToFile(logger logr.Logger, secrets []*corev1.Secret) error {
	// image registry secrets are always stored with the key .dockerconfigjson or .dockercfg
	// ref: https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/#registry-secret-existing-credentials
	// expected format for auth.json
	// ref: https://github.com/containers/image/blob/main/docs/containers-auth.json.5.md

	jsonData := dockerConfigJSON{}
	jsonData.Auths = make(dockerCfg)

	for _, s := range secrets {
		if secretData, ok := s.Data[".dockerconfigjson"]; ok {
			// process as dockerconfigjson
			dcj := &dockerConfigJSON{}
			if err := json.Unmarshal(secretData, dcj); err != nil {
				return err
			}
			for n, v := range dcj.Auths {
				jsonData.Auths[n] = v
			}
			continue
		}
		if secretData, ok := s.Data[".dockercfg"]; ok {
			// process as dockercfg, despite being a map, this has to be Unmarshal'd as a pointer
			dc := &dockerCfg{}
			if err := json.Unmarshal(secretData, dc); err != nil {
				return err
			}
			for n, v := range *dc {
				jsonData.Auths[n] = v
			}
			continue
		}
		// Ignore the unknown secret
		logger.Info("expected secret.Data key not found", "secret", types.NamespacedName{Name: s.Name, Namespace: s.Namespace})
	}

	data, err := json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal secret data: %w", err)
	}
	err = renameio.WriteFile(r.AuthFilePath, data, 0600)
	if err != nil {
		return fmt.Errorf("failed to write secret data to file: %w", err)
	}
	logger.Info("saved global pull secret data locally")
	return nil
}

// deleteSecretFile deletes the auth file if the secret is deleted
func (r *PullSecretReconciler) deleteSecretFile(logger logr.Logger) error {
	logger.Info("deleting local auth file", "file", r.AuthFilePath)
	if err := os.Remove(r.AuthFilePath); err != nil {
		if os.IsNotExist(err) {
			logger.Info("auth file does not exist, nothing to delete")
			return nil
		}
		return fmt.Errorf("failed to delete secret file: %w", err)
	}
	logger.Info("auth file deleted successfully")
	return nil
}
