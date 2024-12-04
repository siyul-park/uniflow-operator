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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	uniflowdevv1 "github.com/siyul-park/uniflow-operator/api/v1"
)

var _ = Describe("Revision Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		revision := &uniflowdevv1.Revision{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Revision")
			err := k8sClient.Get(ctx, typeNamespacedName, revision)
			if err != nil && errors.IsNotFound(err) {
				resource := &uniflowdevv1.Revision{
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
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &uniflowdevv1.Revision{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Revision")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			reconciler := &RevisionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			revision := &uniflowdevv1.Revision{}
			err = k8sClient.Get(ctx, typeNamespacedName, revision)
			Expect(err).NotTo(HaveOccurred())

			var deployment appsv1.Deployment
			err = k8sClient.Get(ctx, types.NamespacedName{Namespace: revision.Namespace, Name: revision.Status.LastCreatedDeploymentName}, &deployment)
			Expect(err).NotTo(HaveOccurred())

			var service corev1.Service
			err = k8sClient.Get(ctx, types.NamespacedName{Namespace: revision.Namespace, Name: revision.Status.LastCreatedServiceName}, &service)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
