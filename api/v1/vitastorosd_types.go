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

// VitastorOSDSpec defines the desired state of VitastorOSD
type VitastorOSDSpec struct {
	// Name of node
	NodeName string `json:"nodeName"`
	// Path to OSD disk (i.e. /dev/disk/by-partuuid/<...>)
	OSDPath string `json:"osdPath"`
	// Number allocated to OSD
	OSDNumber int `json:"osdNumber"`
	// // Tags that applied to OSD (divided by comma, i.e. "hostN,nvme,dcN")
	// +optional
	OSDTags string `json:"osdTags"`
}

// VitastorOSDStatus defines the observed state of VitastorOSD
type VitastorOSDStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// VitastorOSD is the Schema for the vitastorosds API
type VitastorOSD struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VitastorOSDSpec   `json:"spec,omitempty"`
	Status VitastorOSDStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VitastorOSDList contains a list of VitastorOSD
type VitastorOSDList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VitastorOSD `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VitastorOSD{}, &VitastorOSDList{})
}
