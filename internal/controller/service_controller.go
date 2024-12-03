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
	"k8s.io/apimachinery/pkg/runtime/schema"
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
		return ctrl.Result{}, fmt.Errorf("no kinds found for Service")
	}

	revision := r.createRevision(service, kinds[0])
	if err := r.Create(ctx, &revision); err != nil {
		logger.Error(err, "Failed to create new Revision")
		return ctrl.Result{}, err
	}
	logger.Info("Created new Revision", "RevisionName", revision.Name)

	service.Status.LastCreatedRevisionName = revision.Name
	if err := r.Status().Update(ctx, &service); err != nil {
		logger.Error(err, "Failed to update Service status")
		return ctrl.Result{}, err
	}

	if err := r.cleanupRevisions(ctx, service); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Reconciliation complete", "RevisionName", revision.Name)
	return ctrl.Result{}, nil
}

// Helper method to create a new Revision from the Service
func (r *ServiceReconciler) createRevision(service uniflowdevv1.Service, kind schema.GroupVersionKind) uniflowdevv1.Revision {
	revision := uniflowdevv1.Revision{
		ObjectMeta: service.Spec.Template.ObjectMeta,
		Spec:       service.Spec.Template.Spec,
	}

	revision.GenerateName = service.Name + "-revision-"
	revision.Namespace = service.Namespace
	revision.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: kind.GroupVersion().String(),
			Kind:       kind.Kind,
			Name:       service.Name,
			UID:        service.UID,
		},
	}

	return revision
}

// Helper method to clean up unused Revisions
func (r *ServiceReconciler) cleanupRevisions(ctx context.Context, service uniflowdevv1.Service) error {
	var revisionsList uniflowdevv1.RevisionList
	if err := r.List(ctx, &revisionsList, client.InNamespace(service.Namespace)); err != nil {
		return err
	}

	for _, rev := range revisionsList.Items {
		isOwned := false
		for _, owner := range rev.OwnerReferences {
			if owner.UID == service.UID {
				isOwned = true
				break
			}
		}

		if isOwned && rev.Name != service.Status.LastCreatedRevisionName {
			if err := r.Delete(ctx, &rev); err != nil {
				return err
			}
			log.FromContext(ctx).Info("Deleted unused Revision", "RevisionName", rev.Name)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uniflowdevv1.Service{}).
		Complete(r)
}
