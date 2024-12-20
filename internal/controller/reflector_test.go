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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/google/uuid"
	"github.com/siyul-park/uniflow-operator/internal/spec"
	"golang.org/x/net/websocket"
	corev1 "k8s.io/api/core/v1"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	uniflowdevv1 "github.com/siyul-park/uniflow-operator/api/v1"
)

var _ = Describe("Reflector", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}

		specs := []spec.Spec{
			&spec.Unstructured{
				Meta: spec.Meta{
					ID:        uuid.New(),
					Namespace: "test-ns",
					Name:      "test-name",
				},
			},
		}

		var server *httptest.Server

		BeforeEach(func() {
			By("creating the custom resource for the Kind Revision")
			var resource uniflowdevv1.Revision
			err := k8sClient.Get(ctx, typeNamespacedName, &resource)
			if err != nil && errors.IsNotFound(err) {
				resource = uniflowdevv1.Revision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: uniflowdevv1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-pod",
									Image: "busybox",
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, &resource)).To(Succeed())
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/v1/specs"))

				if r.URL.Query().Get("watch") == "true" {
					handler := func(conn *websocket.Conn) {
						defer func() { _ = conn.Close() }()

						for i := 0; i < 3; i++ {
							event := spec.Event{
								OP: spec.EventStore,
								ID: uuid.New(),
							}
							err := websocket.JSON.Send(conn, event)
							Expect(err).To(Succeed())
							time.Sleep(100 * time.Millisecond)
						}
					}
					websocket.Handler(handler).ServeHTTP(w, r)
				} else {
					Expect(json.NewEncoder(w).Encode(specs)).To(Succeed())
				}
			}))

			resource.Status.LastBindEndpoint = server.URL
			err = k8sClient.Status().Update(ctx, &resource)
			Expect(err).To(Succeed())
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance Revision")

			resource := &uniflowdevv1.Revision{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			server.Close()
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			reflector := &Reflector{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				NamespacedName: typeNamespacedName,
			}
			defer func() { _ = reflector.Close() }()

			err := reflector.Reconcile(ctx, specs[0].GetID())
			Expect(err).NotTo(HaveOccurred())

			var revision uniflowdevv1.Revision
			err = k8sClient.Get(ctx, typeNamespacedName, &revision)
			Expect(err).NotTo(HaveOccurred())
			Expect(revision.Status.LastBindNamespaces).NotTo(BeNil())
			Expect(revision.Status.LastCreatedServingNames).NotTo(BeNil())

			var service servingv1.Service
			err = k8sClient.Get(ctx, types.NamespacedName{Namespace: revision.Namespace, Name: revision.Status.LastCreatedServingNames[specs[0].GetNamespace()]}, &service)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
