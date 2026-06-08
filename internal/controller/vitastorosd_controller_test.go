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

var _ = Describe("VitastorOSD Controller", func() {
	Context("When managing VitastorOSD resources", func() {
		const osdName = "test-osd-1"
		ctx := context.Background()

		AfterEach(func() {
			osd := &controlv2.VitastorOSD{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: osdName}, osd); err == nil {
				Expect(k8sClient.Delete(ctx, osd)).To(Succeed())
			}
		})

		It("should create a VitastorOSD CR with correct spec", func() {
			osd := &controlv2.VitastorOSD{
				ObjectMeta: metav1.ObjectMeta{
					Name: osdName,
					Labels: map[string]string{
						"control.vitastor.io/cluster": "test-cluster",
						"control.vitastor.io/node":    "test-worker-node",
						"control.vitastor.io/disk":    "test-worker-node-sda",
					},
				},
				Spec: controlv2.VitastorOSDSpec{
					Id:     1,
					Path:   "/dev/disk/by-partuuid/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
					Weight: "1.0",
					NoOut:  false,
					Tags:   []string{"ssd", "dc1"},
				},
			}
			Expect(k8sClient.Create(ctx, osd)).To(Succeed())

			fetched := &controlv2.VitastorOSD{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: osdName}, fetched)).To(Succeed())
			Expect(fetched.Spec.Id).To(Equal(int32(1)))
			Expect(fetched.Spec.Weight).To(Equal("1.0"))
			Expect(fetched.Spec.Tags).To(ConsistOf("ssd", "dc1"))
		})

		It("should allow setting noout for maintenance", func() {
			osd := &controlv2.VitastorOSD{
				ObjectMeta: metav1.ObjectMeta{
					Name: osdName,
					Labels: map[string]string{
						"control.vitastor.io/cluster": "test-cluster",
						"control.vitastor.io/node":    "test-worker-node",
					},
				},
				Spec: controlv2.VitastorOSDSpec{
					Id:     1,
					Path:   "/dev/disk/by-partuuid/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
					Weight: "1.0",
					NoOut:  false,
				},
			}
			Expect(k8sClient.Create(ctx, osd)).To(Succeed())

			// Simulate maintenance: set noout and reduce weight
			fetched := &controlv2.VitastorOSD{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: osdName}, fetched)).To(Succeed())
			fetched.Spec.NoOut = true
			fetched.Spec.Weight = "0"
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			updated := &controlv2.VitastorOSD{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: osdName}, updated)).To(Succeed())
			Expect(updated.Spec.NoOut).To(BeTrue())
			Expect(updated.Spec.Weight).To(Equal("0"))
		})
	})
})
