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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VitastorNodeSpec defines the desired state of VitastorNode
type VitastorNodeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// Name of node that have disks for OSDs
	NodeName string `json:"nodeName"`
	// OSD image name/tag
	OSDImage string `json:"osdImage"`
}

// VitastorNodeStatus defines the observed state of VitastorNode.
type VitastorNodeStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the VitastorNode resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	// Conditions []metav1.Condition `json:"conditions,omitempty"`

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
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of VitastorNode
	// +required
	Spec VitastorNodeSpec `json:"spec"`

	// status defines the observed state of VitastorNode
	// +optional
	Status VitastorNodeStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// VitastorNodeList contains a list of VitastorNode
type VitastorNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VitastorNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VitastorNode{}, &VitastorNodeList{})
}
