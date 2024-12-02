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

package controller

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	uniflowdevv1 "github.com/siyul-park/uniflow-operator/api/v1"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=uniflow.dev,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=uniflow.dev,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=uniflow.dev,resources=services/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Service object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	service := uniflowdevv1.Service{}
	if err := r.Get(ctx, req.NamespacedName, &service); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Service")
		return ctrl.Result{}, err
	}

	kinds, _, err := r.Scheme.ObjectKinds(&service)
	if err != nil {
		logger.Error(err, "Failed to get kinds for Service")
		return ctrl.Result{}, err
	}

	if len(kinds) == 0 {
		logger.Error(err, "No kinds found for Service")
		return ctrl.Result{}, fmt.Errorf("no kinds found for service")
	}

	revision := uniflowdevv1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: service.Name + "-rev-",
			Namespace:    service.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: kinds[0].GroupVersion().String(),
					Kind:       kinds[0].Kind,
					Name:       service.Name,
					UID:        service.UID,
				},
			},
		},
		Spec: service.Spec.Template.Spec,
	}

	if err := r.Create(ctx, &revision); err != nil {
		logger.Error(err, "Failed to create new Revision")
		return ctrl.Result{}, err
	}
	logger.Info("Created new Revision", "RevisionName", revision.Name)

	var revisionsList uniflowdevv1.RevisionList
	if err := r.List(ctx, &revisionsList, client.InNamespace(service.Namespace)); err != nil {
		logger.Error(err, "Failed to list Revisions")
		return ctrl.Result{}, err
	}

	for _, rev := range revisionsList.Items {
		isOwned := false
		for _, owner := range rev.OwnerReferences {
			if owner.UID == service.UID {
				isOwned = true
				break
			}
		}

		if isOwned && rev.Name != revision.Name {
			if err := r.Delete(ctx, &rev); err != nil {
				logger.Error(err, "Failed to delete unused Revision", "RevisionName", rev.Name)
				return ctrl.Result{}, err
			}
			logger.Info("Deleted unused Revision", "RevisionName", rev.Name)
		}
	}

	logger.Info("Reconciliation complete", "RevisionName", revision.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uniflowdevv1.Service{}).
		Complete(r)
}
