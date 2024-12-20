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

// Reflector manages the synchronization of revision services and stream events.
type Reflector struct {
	client.Client
	Scheme         *runtime.Scheme
	NamespacedName types.NamespacedName
	done           chan struct{}
	mu             sync.Mutex
}

// Listen processes events from the stream and reconciles states.
func (r *Reflector) Listen(ctx context.Context) error {
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
func (r *Reflector) Reconcile(ctx context.Context, id uuid.UUID) error {
	logger := log.FromContext(ctx)

	var revision uniflowdevv1.Revision
	if err := r.Get(ctx, r.NamespacedName, &revision); err != nil {
		logger.Error(err, "Failed to get Revision")
		return err
	}

	store := spec.NewStore(revision.Status.LastBindEndpoint)

	specs, err := store.Load(ctx, &spec.Meta{ID: id})
	if err != nil {
		logger.Error(err, "Failed to load spec")
		return err
	}

	if revision.Status.LastBindNamespaces == nil {
		revision.Status.LastBindNamespaces = make(map[string]string)
	}
	delete(revision.Status.LastBindNamespaces, id.String())
	for _, spc := range specs {
		revision.Status.LastBindNamespaces[spc.GetID().String()] = spc.GetNamespace()
	}

	if err := r.Client.Status().Update(ctx, &revision); err != nil {
		logger.Error(err, "Failed to update Revision")
		return err
	}

	return r.reflect(ctx, revision)
}

// Done returns the completion signal channel.
func (r *Reflector) Done() <-chan struct{} {
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
func (r *Reflector) Close() error {
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

func (r *Reflector) start(ctx context.Context) (*spec.Stream, error) {
	logger := log.FromContext(ctx)

	var revision uniflowdevv1.Revision
	if err := r.Get(ctx, r.NamespacedName, &revision); err != nil {
		logger.Error(err, "Failed to get Revision")
		return nil, err
	}

	store := spec.NewStore(revision.Status.LastBindEndpoint)
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

	revision.Status.LastBindNamespaces = make(map[string]string)
	for _, spc := range specs {
		revision.Status.LastBindNamespaces[spc.GetID().String()] = spc.GetNamespace()
	}

	if err := r.Client.Status().Update(ctx, &revision); err != nil {
		logger.Error(err, "Failed to update Revision")
		return nil, err
	}

	if err := r.reflect(ctx, revision); err != nil {
		logger.Error(err, "Failed to setup Services")
		return nil, err
	}

	return stream, nil
}

func (r *Reflector) reflect(ctx context.Context, revision uniflowdevv1.Revision) error {
	var namespaces []string
	for _, namespace := range revision.Status.LastBindNamespaces {
		if !slices.Contains(namespaces, namespace) {
			namespaces = append(namespaces, namespace)
		}
	}

	if revision.Status.LastCreatedServingNames == nil {
		revision.Status.LastCreatedServingNames = make(map[string]string)
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

	return r.Client.Status().Update(ctx, &revision)
}

func (r *Reflector) createService(revision uniflowdevv1.Revision, namespace string) (servingv1.Service, error) {
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
				"app.kubernetes.io/part-of":  revision.Name,
				"app.kubernetes.io/instance": revision.Name + "-" + namespace,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: kinds[0].GroupVersion().String(),
					Kind:       kinds[0].Kind,
					Name:       revision.Name,
					UID:        revision.UID,
				},
			},
		},
		Spec: servingv1.ServiceSpec{
			ConfigurationSpec: servingv1.ConfigurationSpec{
				Template: servingv1.RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app.kubernetes.io/part-of":  revision.Name,
							"app.kubernetes.io/instance": revision.Name + "-" + namespace,
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
