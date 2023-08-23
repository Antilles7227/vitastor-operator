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
	//corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VitastorNodeSpec defines the desired state of VitastorNode
type VitastorNodeSpec struct {
	// Name of node that have disks for OSDs
	NodeName string `json:"nodeName"`
	// OSD image name/tag
	OSDImage string `json:"osdImage"`
}

// VitastorNodeStatus defines the observed state of VitastorNode
type VitastorNodeStatus struct {
	// List of disks on that node
	// +optional
	Disks []string `json:"disks"`

	// List of empty disks (without any partition) on that node
	// +optional
	EmptyDisks []string `json:"emptyDisks"`

	// List of Vitastor OSDs on that node
	// +optional
	VitastorDisks []string `json:"vitastorDisks"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// VitastorNode is the Schema for the vitastornodes API
type VitastorNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VitastorNodeSpec   `json:"spec,omitempty"`
	Status VitastorNodeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VitastorNodeList contains a list of VitastorNode
type VitastorNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VitastorNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VitastorNode{}, &VitastorNodeList{})
}
