/*
Copyright 2025.

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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
)

var _ = Describe("VitastorPool Controller", func() {
	Context("When managing VitastorPool resources", func() {
		const poolName = "test-pool"
		ctx := context.Background()

		AfterEach(func() {
			pool := &controlv2.VitastorPool{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: poolName}, pool); err == nil {
				// Remove finalizer if present so delete works
				pool.Finalizers = nil
				_ = k8sClient.Update(ctx, pool)
				_ = k8sClient.Delete(ctx, pool)
			}
		})

		It("should create a VitastorPool CR with correct spec", func() {
			pool := &controlv2.VitastorPool{
				ObjectMeta: metav1.ObjectMeta{
					Name: poolName,
				},
				Spec: controlv2.VitastorPoolSpec{
					Name:       poolName,
					Scheme:     "replicated",
					PGSize:     2,
					PGMinSize:  1,
					PGCount:    32,
					VitastorFS: false,
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			fetched := &controlv2.VitastorPool{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: poolName}, fetched)).To(Succeed())
			Expect(fetched.Spec.Scheme).To(Equal("replicated"))
			Expect(fetched.Spec.PGSize).To(Equal(int32(2)))
			Expect(fetched.Spec.PGCount).To(Equal(int32(32)))
		})

		It("should fail reconciliation gracefully when vitastor.conf is not available", func() {
			pool := &controlv2.VitastorPool{
				ObjectMeta: metav1.ObjectMeta{
					Name: poolName,
				},
				Spec: controlv2.VitastorPoolSpec{
					Name:      poolName,
					Scheme:    "replicated",
					PGSize:    2,
					PGMinSize: 1,
					PGCount:   32,
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			reconciler := &VitastorPoolReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Reconcile will fail because /etc/vitastor/vitastor.conf doesn't exist in test env
			// but the CR itself should be intact
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: poolName},
			})
			// Error expected (no vitastor.conf), but k8s CR operations are valid
			Expect(err).To(HaveOccurred())

			// CR should still exist
			fetched := &controlv2.VitastorPool{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: poolName}, fetched)).To(Succeed())
		})
	})
})
