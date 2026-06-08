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

var _ = Describe("VitastorCluster Controller", func() {
	Context("When creating a VitastorCluster", func() {
		const clusterName = "test-cluster"
		ctx := context.Background()

		AfterEach(func() {
			cluster := &controlv2.VitastorCluster{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName}, cluster); err == nil {
				Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
			}
		})

		It("should create the VitastorCluster CR successfully", func() {
			cluster := &controlv2.VitastorCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: controlv2.VitastorClusterSpec{
					VitastorNodeLabel:        "vitastor-node",
					VitastorClusterNamespace: "vitastor-system",
					ReconcilePeriodMin:       5,
					Agent: controlv2.AgentSpec{
						Image: "vitalif/vitastor-csi:v2.4.0",
					},
					Monitor: controlv2.MonitorSpec{
						Image:    "vitalif/vitastor:v2.4.0",
						Replicas: 3,
					},
					OSD: controlv2.OSDSpec{
						Image: "vitalif/vitastor:v2.4.0",
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Verify it can be fetched
			fetched := &controlv2.VitastorCluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName}, fetched)).To(Succeed())
			Expect(fetched.Spec.VitastorNodeLabel).To(Equal("vitastor-node"))
			Expect(fetched.Spec.Monitor.Replicas).To(Equal(int32(3)))
		})
	})
})
