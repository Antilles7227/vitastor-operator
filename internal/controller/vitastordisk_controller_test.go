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

var _ = Describe("VitastorDisk Controller", func() {
	Context("When managing VitastorDisk resources", func() {
		const diskName = "test-worker-node-sda"
		ctx := context.Background()

		AfterEach(func() {
			disk := &controlv2.VitastorDisk{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: diskName}, disk); err == nil {
				Expect(k8sClient.Delete(ctx, disk)).To(Succeed())
			}
		})

		It("should create a VitastorDisk with Discovered state", func() {
			disk := &controlv2.VitastorDisk{
				ObjectMeta: metav1.ObjectMeta{
					Name: diskName,
					Labels: map[string]string{
						"control.vitastor.io/cluster": "test-cluster",
						"control.vitastor.io/node":    "test-worker-node",
					},
				},
				Spec: controlv2.VitastorDiskSpec{
					DevicePath:   "/dev/sda",
					NodeRef:      "test-worker-node",
					DesiredState: controlv2.DiskStateDiscovered,
				},
			}
			Expect(k8sClient.Create(ctx, disk)).To(Succeed())

			fetched := &controlv2.VitastorDisk{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: diskName}, fetched)).To(Succeed())
			Expect(fetched.Spec.DevicePath).To(Equal("/dev/sda"))
			Expect(fetched.Spec.DesiredState).To(Equal(controlv2.DiskStateDiscovered))
		})

		It("should transition disk to Prepared state via spec update", func() {
			disk := &controlv2.VitastorDisk{
				ObjectMeta: metav1.ObjectMeta{
					Name: diskName,
					Labels: map[string]string{
						"control.vitastor.io/cluster": "test-cluster",
						"control.vitastor.io/node":    "test-worker-node",
					},
				},
				Spec: controlv2.VitastorDiskSpec{
					DevicePath:      "/dev/sda",
					NodeRef:         "test-worker-node",
					DesiredState:    controlv2.DiskStateDiscovered,
					DesiredOSDCount: 1,
				},
			}
			Expect(k8sClient.Create(ctx, disk)).To(Succeed())

			// Simulate kubectl vitastor disk prepare
			fetched := &controlv2.VitastorDisk{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: diskName}, fetched)).To(Succeed())
			fetched.Spec.DesiredState = controlv2.DiskStatePrepared
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			updated := &controlv2.VitastorDisk{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: diskName}, updated)).To(Succeed())
			Expect(updated.Spec.DesiredState).To(Equal(controlv2.DiskStatePrepared))
		})

		It("should transition disk to Decommissioned state via spec update", func() {
			disk := &controlv2.VitastorDisk{
				ObjectMeta: metav1.ObjectMeta{
					Name: diskName,
					Labels: map[string]string{
						"control.vitastor.io/cluster": "test-cluster",
						"control.vitastor.io/node":    "test-worker-node",
					},
				},
				Spec: controlv2.VitastorDiskSpec{
					DevicePath:   "/dev/sda",
					NodeRef:      "test-worker-node",
					DesiredState: controlv2.DiskStatePrepared,
				},
			}
			Expect(k8sClient.Create(ctx, disk)).To(Succeed())

			// Simulate kubectl vitastor disk remove
			fetched := &controlv2.VitastorDisk{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: diskName}, fetched)).To(Succeed())
			fetched.Spec.DesiredState = controlv2.DiskStateDecommissioned
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			updated := &controlv2.VitastorDisk{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: diskName}, updated)).To(Succeed())
			Expect(updated.Spec.DesiredState).To(Equal(controlv2.DiskStateDecommissioned))
		})
	})
})
