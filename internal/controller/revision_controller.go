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
	"time"

	uniflowdevv1 "github.com/siyul-park/uniflow-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// RevisionReconciler reconciles a Revision object
type RevisionReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	reflectors map[types.NamespacedName]*Reflector
}

// +kubebuilder:rbac:groups=uniflow.dev,resources=revisions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=uniflow.dev,resources=revisions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=uniflow.dev,resources=revisions/finalizers,verbs=update

// Load is a method that loads and manages the revisions and their corresponding reflectors.
func (r *RevisionReconciler) Load(ctx context.Context) error {
	logger := log.FromContext(ctx)

	var revisionList uniflowdevv1.RevisionList
	if err := r.List(ctx, &revisionList); err != nil {
		logger.Error(err, "Failed to list revisions")
		return err
	}

	for _, rev := range revisionList.Items {
		if err := r.listen(rev); err != nil {
			logger.Error(err, "Failed to listen")
			return err
		}
	}

	for namespacedName, reflector := range r.reflectors {
		exists := false
		for _, rev := range revisionList.Items {
			if (types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name}) == namespacedName {
				exists = true
				break
			}
		}

		if !exists {
			delete(r.reflectors, namespacedName)
			if err := reflector.Close(); err != nil {
				return err
			}
		}
	}

	return nil
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Revision object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *RevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	revision := uniflowdevv1.Revision{}
	if err := r.Get(ctx, req.NamespacedName, &revision); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Revision")
		return ctrl.Result{}, err
	}

	deployment, err := r.createDeployment(revision)
	if err != nil {
		logger.Error(err, "Failed to create Deployment")
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, &deployment); err != nil {
		logger.Error(err, "Failed to create Deployment", "DeploymentName", deployment.Name)
		return ctrl.Result{}, err
	}

	revision.Status.LastCreatedDeploymentName = deployment.Name
	if err := r.Status().Update(ctx, &revision); err != nil {
		logger.Error(err, "Failed to update Revision status")
		return ctrl.Result{}, err
	}

	if err := r.cleanupDeployments(ctx, revision); err != nil {
		return ctrl.Result{}, err
	}

	service, err := r.createService(revision)
	if err != nil {
		logger.Error(err, "Failed to create Service")
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, &service); err != nil {
		logger.Error(err, "Failed to create Service", "ServiceName", service.Name)
		return ctrl.Result{}, err
	}

	revision.Status.LastCreatedServiceName = service.Name
	revision.Status.LastBindEndpoint = r.discovery(&service)

	if err := r.Status().Update(ctx, &revision); err != nil {
		logger.Error(err, "Failed to update Revision status")
		return ctrl.Result{}, err
	}

	if err := r.cleanupServices(ctx, revision); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Created new Deployment and Service", "DeploymentName", deployment.Name, "ServiceName", service.Name)

	if err := r.listen(revision); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RevisionReconciler) createDeployment(revision uniflowdevv1.Revision) (appsv1.Deployment, error) {
	kinds, _, err := r.Scheme.ObjectKinds(&revision)
	if err != nil || len(kinds) == 0 {
		return appsv1.Deployment{}, fmt.Errorf("no kinds found for Revision")
	}

	containers := make([]corev1.Container, len(revision.Spec.Containers))
	for i, container := range revision.Spec.Containers {
		container.Args = append(container.Args, "--namespace", "system", "--env", "PORT=8000")
		containers[i] = container
	}

	podSpec := revision.Spec.PodSpec
	podSpec.Containers = containers

	return appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: revision.Name + "-deployment-system",
			Namespace:    revision.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: kinds[0].GroupVersion().String(),
					Kind:       kinds[0].Kind,
					Name:       revision.Name,
					UID:        revision.UID,
				},
			},
			Labels: map[string]string{
				"app.kubernetes.io/part-of":  revision.Name,
				"app.kubernetes.io/instance": revision.Name + "-system",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/instance": revision.Name + "-system",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/part-of":  revision.Name,
						"app.kubernetes.io/instance": revision.Name + "-system",
					},
				},
				Spec: podSpec,
			},
		},
	}, nil
}

func (r *RevisionReconciler) createService(revision uniflowdevv1.Revision) (corev1.Service, error) {
	kinds, _, err := r.Scheme.ObjectKinds(&revision)
	if err != nil || len(kinds) == 0 {
		return corev1.Service{}, fmt.Errorf("no kinds found for Revision")
	}

	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: revision.Name + "-service-system",
			Namespace:    revision.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: kinds[0].GroupVersion().String(),
					Kind:       kinds[0].Kind,
					Name:       revision.Name,
					UID:        revision.UID,
				},
			},
			Labels: map[string]string{
				"app.kubernetes.io/part-of":  revision.Name,
				"app.kubernetes.io/instance": revision.Name + "-system",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/instance": revision.Name + "-system",
			},
			Ports: []corev1.ServicePort{
				{
					Port:       8000,
					TargetPort: intstr.FromInt32(8000),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}, nil
}

// Helper method to clean up unused deployments
func (r *RevisionReconciler) cleanupDeployments(ctx context.Context, revision uniflowdevv1.Revision) error {
	var deploymentList appsv1.DeploymentList
	if err := r.List(ctx, &deploymentList, client.InNamespace(revision.Namespace)); err != nil {
		return err
	}

	for _, deploy := range deploymentList.Items {
		isOwned := false
		for _, owner := range deploy.OwnerReferences {
			if owner.UID == revision.UID {
				isOwned = true
				break
			}
		}

		if isOwned && deploy.Name != revision.Status.LastCreatedDeploymentName {
			if err := r.Delete(ctx, &deploy); err != nil {
				return err
			}
			log.FromContext(ctx).Info("Deleted unused Deployment", "DeploymentName", deploy.Name)
		}
	}
	return nil
}

// Helper method to clean up unused services
func (r *RevisionReconciler) cleanupServices(ctx context.Context, revision uniflowdevv1.Revision) error {
	var serviceList corev1.ServiceList
	if err := r.List(ctx, &serviceList, client.InNamespace(revision.Namespace)); err != nil {
		return err
	}

	for _, svc := range serviceList.Items {
		isOwned := false
		for _, owner := range svc.OwnerReferences {
			if owner.UID == revision.UID {
				isOwned = true
				break
			}
		}

		if isOwned && svc.Name != revision.Status.LastCreatedServiceName {
			if err := r.Delete(ctx, &svc); err != nil {
				return err
			}
			log.FromContext(ctx).Info("Deleted unused Service", "ServiceName", svc.Name)
		}
	}
	return nil
}

func (r *RevisionReconciler) listen(revision uniflowdevv1.Revision) error {
	namespacedName := types.NamespacedName{Namespace: revision.Namespace, Name: revision.Name}

	if r.reflectors == nil {
		r.reflectors = make(map[types.NamespacedName]*Reflector)
	}

	reflector, ok := r.reflectors[namespacedName]
	if !ok {
		reflector = &Reflector{
			Client:         r.Client,
			Scheme:         r.Scheme,
			NamespacedName: namespacedName,
		}
		r.reflectors[namespacedName] = reflector

		go func() {
			ctx := context.Background()

			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					_ = reflector.Listen(ctx)
				case <-reflector.Done():
					return
				}
			}
		}()
	}

	if revision.DeletionTimestamp != nil {
		delete(r.reflectors, namespacedName)
		if err := reflector.Close(); err != nil {
			return err
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RevisionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uniflowdevv1.Revision{}).
		Complete(r)
}

func (r *RevisionReconciler) discovery(svc *corev1.Service) string {
	if svc == nil || svc.Name == "" || svc.Namespace == "" || len(svc.Spec.Ports) == 0 {
		return ""
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", svc.Name, svc.Namespace, svc.Spec.Ports[0].Port)
}
