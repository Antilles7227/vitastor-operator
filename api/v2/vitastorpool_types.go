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

package v2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VitastorPoolSpec defines the desired state of VitastorPool
// +kubebuilder:validation:XValidation:rule="self.scheme in ['xor', 'ec', 'jerasure'] ? has(self.parityChunks) : true",message="ParityChunks is required when scheme is xor, ec, or jerasure"
type VitastorPoolSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	Name       string `json:"name"`
	VitastorFS bool   `json:"vitastorFS"`
	// +kubebuilder:validation:Enum=replicated;xor;ec;jerasure
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Scheme is immutable"
	Scheme string `json:"scheme"`
	PGSize int32  `json:"pgSize"`
	// +optional
	// +kubebuilder:validation:Minimum=1
	ParityChunks *int32 `json:"parityChunks,omitempty"`
	// +kubebuilder:validation:Minimum=1
	PGMinSize          int32   `json:"pgMinSize"`
	PGCount            int32   `json:"pgCount"`
	FailureDomain      *string `json:"failureDomain,omitempty"`
	LevelPlacement     *string `json:"levelPlacement,omitempty"`
	RawPlacement       *string `json:"rawPlacement,omitempty"`
	LocalReads         *string `json:"localReads,omitempty"`
	MaxOSDCombinations *int32  `json:"maxOSDCombinations,omitempty"`
	BlockSize          *int32  `json:"blockSize,omitempty"`
	BitmapGranularity  *int32  `json:"bitmapGranularity,omitempty"`
	ImmediateCommit    *string `json:"immediateCommit,omitempty"`
	OSDTags            *string `json:"osdTags,omitempty"`
	ScrubInterval      *string `json:"scrubInterval,omitempty"`
}

// VitastorPoolStatus defines the observed state of VitastorPool.
type VitastorPoolStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the VitastorPool resource.
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
	Conditions  []metav1.Condition `json:"conditions,omitempty"`
	ID          int32              `json:"id"`
	Total       int64              `json:"totalBytes"`
	Used        int64              `json:"usedBytes"`
	Available   int64              `json:"availableBytes"`
	UsedPercent string             `json:"usedPercent"`
	Efficiency  string             `json:"efficiency"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Cluster

// VitastorPool is the Schema for the vitastorpools API
type VitastorPool struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of VitastorPool
	// +required
	Spec VitastorPoolSpec `json:"spec"`

	// status defines the observed state of VitastorPool
	// +optional
	Status VitastorPoolStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// VitastorPoolList contains a list of VitastorPool
type VitastorPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VitastorPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VitastorPool{}, &VitastorPoolList{})
}
