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

	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
)

var _ = Describe("VitastorNode Controller", func() {
	Context("When managing VitastorNode resources", func() {
		const nodeName = "test-worker-node"
		ctx := context.Background()

		AfterEach(func() {
			node := &controlv2.VitastorNode{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, node); err == nil {
				Expect(k8sClient.Delete(ctx, node)).To(Succeed())
			}
		})

		It("should create a VitastorNode with correct defaults", func() {
			node := &controlv2.VitastorNode{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
					Labels: map[string]string{
						"control.vitastor.io/cluster": "test-cluster",
					},
				},
				Spec: controlv2.VitastorNodeSpec{
					NoOut:  false,
					Weight: "1.0",
				},
			}
			Expect(k8sClient.Create(ctx, node)).To(Succeed())

			fetched := &controlv2.VitastorNode{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, fetched)).To(Succeed())
			Expect(fetched.Spec.NoOut).To(BeFalse())
			Expect(fetched.Spec.Weight).To(Equal("1.0"))
		})

		It("should allow updating NoOut on VitastorNode", func() {
			node := &controlv2.VitastorNode{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
					Labels: map[string]string{
						"control.vitastor.io/cluster": "test-cluster",
					},
				},
				Spec: controlv2.VitastorNodeSpec{
					NoOut:  false,
					Weight: "1.0",
				},
			}
			Expect(k8sClient.Create(ctx, node)).To(Succeed())

			// Enable maintenance mode
			fetched := &controlv2.VitastorNode{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, fetched)).To(Succeed())
			fetched.Spec.NoOut = true
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			updated := &controlv2.VitastorNode{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, updated)).To(Succeed())
			Expect(updated.Spec.NoOut).To(BeTrue())
		})
	})
})
