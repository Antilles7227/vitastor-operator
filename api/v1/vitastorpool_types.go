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

// VitastorPoolSpec defines the desired state of VitastorPool
type VitastorPoolSpec struct {
	// Foo is an example field of VitastorPool. Edit vitastorpool_types.go to remove/update
	ID                 string `json:"id"`
	Scheme             string `json:"scheme"`
	PGSize             int32  `json:"pgSize"`
	ParityChunks       int32  `json:"parityChunks,omitempty"`
	PGMinSize          int32  `json:"pgMinSize"`
	PGCount            int32  `json:"pgCount"`
	FailureDomain      string `json:"failureDomain"`
	MaxOSDCombinations int32  `json:"maxOSDCombinations,omitempty"`
	BlockSize          int32  `json:"blockSize,omitempty"`
	ImmediateCommit    string `json:"immediateCommit,omitempty"`
	OSDTags            string `json:"osdTags,omitempty"`
}

// VitastorPoolStatus defines the observed state of VitastorPool
type VitastorPoolStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// VitastorPool is the Schema for the vitastorpools API
type VitastorPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VitastorPoolSpec   `json:"spec,omitempty"`
	Status VitastorPoolStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VitastorPoolList contains a list of VitastorPool
type VitastorPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VitastorPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VitastorPool{}, &VitastorPoolList{})
}
