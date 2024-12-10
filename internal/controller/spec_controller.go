package controller

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"
	uniflowdevv1 "github.com/siyul-park/uniflow-operator/api/v1"
	"github.com/siyul-park/uniflow-operator/internal/spec"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const ClusterLocalDNSFormat = "%s.%s.svc.cluster.local:%s"

// SpecReconciler manages the synchronization of revision services and stream events.
type SpecReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	NamespacedName types.NamespacedName
	done           chan struct{}
	mu             sync.Mutex
}

// Listen processes events from the stream and reconciles states.
func (r *SpecReconciler) Listen(ctx context.Context) error {
	logger := log.FromContext(ctx)

	stream, err := r.start(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()

	var queue []uuid.UUID
	for {
		select {
		case <-r.Done():
			return nil
		case event, ok := <-stream.Next():
			if !ok {
				return nil
			}
			queue = append(queue, event.ID)

			for i := 0; i < len(queue); i++ {
				if err := r.Reconcile(ctx, queue[i]); err == nil {
					queue = append(queue[:i], queue[i+1:]...)
					i--
				} else {
					logger.Error(err, "Failed to reconcile event", "eventID", queue[i])
				}
			}
		}
	}
}

// Reconcile reconciles state and updates namespaces.
func (r *SpecReconciler) Reconcile(ctx context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	logger := log.FromContext(ctx)

	var revision uniflowdevv1.Revision
	if err := r.Get(ctx, r.NamespacedName, &revision); err != nil {
		logger.Error(err, "Failed to get Revision")
		return err
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: revision.Namespace, Name: revision.Status.LastCreatedServiceName}, service); err != nil {
		logger.Error(err, "Failed to get Service")
		return err
	}

	store := spec.NewStore(fmt.Sprintf(ClusterLocalDNSFormat, service.Name, service.Namespace, service.Spec.ClusterIP))

	specs, err := store.Load(ctx, &spec.Meta{ID: id})
	if err != nil {
		logger.Error(err, "Failed to load spec")
		return err
	}

	if revision.Status.LastBinedNamspases == nil {
		revision.Status.LastBinedNamspases = make(map[string]string)
	}
	delete(revision.Status.LastBinedNamspases, id.String())
	for _, spc := range specs {
		revision.Status.LastBinedNamspases[spc.GetID().String()] = spc.GetNamespace()
	}

	if err := r.Client.Update(ctx, &revision); err != nil {
		logger.Error(err, "Failed to update Revision")
		return err
	}

	return r.reflectService(ctx, revision)
}

// Done returns the completion signal channel.
func (r *SpecReconciler) Done() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	done := r.done
	if done == nil {
		done = make(chan struct{})
		r.done = done
	}
	return done
}

// Close shuts down the reconciler and releases resources.
func (r *SpecReconciler) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.done == nil {
		return nil
	}

	select {
	case <-r.done:
	default:
		close(r.done)
	}
	return nil
}

// start initializes services and starts the stream.
func (r *SpecReconciler) start(ctx context.Context) (*spec.Stream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	logger := log.FromContext(ctx)

	var revision uniflowdevv1.Revision
	if err := r.Get(ctx, r.NamespacedName, &revision); err != nil {
		logger.Error(err, "Failed to get Revision")
		return nil, err
	}

	var service corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Namespace: revision.Namespace, Name: revision.Status.LastCreatedServiceName}, &service); err != nil {
		logger.Error(err, "Failed to get Service")
		return nil, err
	}

	store := spec.NewStore(fmt.Sprintf(ClusterLocalDNSFormat, service.Name, service.Namespace, service.Spec.ClusterIP))
	stream, err := store.Watch(ctx)
	if err != nil {
		logger.Error(err, "Failed to watch stream")
		return nil, err
	}

	specs, err := store.Load(ctx)
	if err != nil {
		logger.Error(err, "Failed to load specs")
		return nil, err
	}

	revision.Status.LastBinedNamspases = make(map[string]string)
	for _, spc := range specs {
		revision.Status.LastBinedNamspases[spc.GetID().String()] = spc.GetNamespace()
	}

	if err := r.Client.Update(ctx, &revision); err != nil {
		logger.Error(err, "Failed to update Revision")
		return nil, err
	}

	if err := r.reflectService(ctx, revision); err != nil {
		logger.Error(err, "Failed to setup Services")
		return nil, err
	}

	return stream, nil
}

// reflectService manages service setup and cleanup.
func (r *SpecReconciler) reflectService(ctx context.Context, revision uniflowdevv1.Revision) error {
	var namespaces []string
	for _, namespace := range revision.Status.LastBinedNamspases {
		if !slices.Contains(namespaces, namespace) {
			namespaces = append(namespaces, namespace)
		}
	}

	for _, namespace := range namespaces {
		if _, ok := revision.Status.LastCreatedServingNames[namespace]; ok {
			continue
		}

		service, err := r.createService(revision, namespace)
		if err != nil {
			return err
		}
		if err := r.Client.Create(ctx, &service); err != nil {
			return err
		}

		revision.Status.LastCreatedServingNames[namespace] = service.Name
	}

	for namespace, serviceName := range revision.Status.LastCreatedServingNames {
		if !slices.Contains(namespaces, namespace) {
			var service servingv1.Service
			if err := r.Get(ctx, types.NamespacedName{Namespace: revision.Namespace, Name: serviceName}, &service); err == nil {
				if err := r.Client.Delete(ctx, &service); err != nil {
					return err
				}
			}
			delete(revision.Status.LastCreatedServingNames, namespace)
		}
	}

	return r.Client.Update(ctx, &revision)
}

// createService generates a new service based on the revision and namespace.
func (r *SpecReconciler) createService(revision uniflowdevv1.Revision, namespace string) (servingv1.Service, error) {
	kinds, _, err := r.Scheme.ObjectKinds(&revision)
	if err != nil || len(kinds) == 0 {
		return servingv1.Service{}, fmt.Errorf("no kinds found for Service")
	}

	containers := make([]corev1.Container, len(revision.Spec.Containers))
	for i, container := range revision.Spec.Containers {
		container.Args = append(container.Args, "--namespace", namespace, "--env", "PORT=$(PORT)")
		containers[i] = container
	}

	podSpec := revision.Spec.PodSpec
	podSpec.Containers = containers

	return servingv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-serving-%s", revision.Name, namespace),
			Namespace:    revision.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":  kinds[0].Group + "-" + revision.Name,
				"app.kubernetes.io/instance": kinds[0].Group + "-" + revision.Name + "-" + namespace,
			},
		},
		Spec: servingv1.ServiceSpec{
			ConfigurationSpec: servingv1.ConfigurationSpec{
				Template: servingv1.RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app.kubernetes.io/part-of":  kinds[0].Group + "-" + revision.Name,
							"app.kubernetes.io/instance": kinds[0].Group + "-" + revision.Name + "-" + namespace,
						},
					},
					Spec: servingv1.RevisionSpec{
						PodSpec: podSpec,
					},
				},
			},
		},
	}, nil
}
