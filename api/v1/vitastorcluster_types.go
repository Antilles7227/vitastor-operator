/*
Copyright 2022.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VitastorClusterSpec defines the desired state of VitastorCluster
type VitastorClusterSpec struct {
	// Number of replicas for monitors
	MonitorReplicaNum int `json:"monitorReplicaNum"`
	// Node label for Agent DaemonSet nodeSelector
	VitastorNodeLabel string `json:"vitastorNodeLabel"`
	// Reconcile period in seconds
	DisksReconciligPeriod int `json:"disksReconciligPeriod"`
	// Agent image name/tag
	AgentImage string `json:"agentImage"`
	// Monitor image name/tag
	MonitorImage string `json:"monitorImage"`
	// OSD image name/tag
	OSDImage string `json:"osdImage"`
}

// VitastorClusterStatus defines the observed state of VitastorCluster
type VitastorClusterStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// VitastorCluster is the Schema for the vitastorclusters API
type VitastorCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VitastorClusterSpec   `json:"spec,omitempty"`
	Status VitastorClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VitastorClusterList contains a list of VitastorCluster
type VitastorClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VitastorCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VitastorCluster{}, &VitastorClusterList{})
}
