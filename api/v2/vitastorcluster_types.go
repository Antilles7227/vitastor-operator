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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type MonitorSpec struct {
	Image     string                      `json:"image"`
	Replicas  int32                       `json:"replicas"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

type AgentSpec struct {
	Image     string                      `json:"image"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	Debug     bool                        `json:"debug,omitempty"`
}
type OSDSpec struct {
	Image                  string                      `json:"image"`
	Resources              corev1.ResourceRequirements `json:"resources,omitempty"`
	EtcdReportIntervalSecs int32                       `json:"etcdReportIntervalSecs,omitempty"`
	EtcdStatsIntervalSecs  int32                       `json:"etcdStatsIntervalSecs,omitempty"`
	AutosyncIntervalSecs   int32                       `json:"autosyncIntervalSecs,omitempty"`
	AutosyncWrites         int32                       `json:"autosyncWrites,omitempty"`
	RecoveryQueueDepth     int32                       `json:"recoveryQueueDepth,omitempty"`
	RecoverySleepUs        int32                       `json:"recoverySleepUs,omitempty"`
	RecoveryPGSwitch       int32                         `json:"recoveryPgSwitch,omitempty"`  // number of PGs to recover at once
	RecoverySyncBatch      int32                         `json:"recoverySyncBatch,omitempty"` // batch size for sync
	NoRecovery             bool                        `json:"noRecovery,omitempty"`        // disable recovery
	NoRebalance            bool                        `json:"noRebalance,omitempty"`       // disable automatic rebalancing

	PrintStatsIntervalSecs int32 `json:"printStatsIntervalSecs,omitempty"` // e.g., "10s", "1m"
	SlowLogIntervalSecs    int32 `json:"slowLogIntervalSecs,omitempty"`    // interval to print slow ops

	AutoScrub            bool   `json:"autoScrub,omitempty"`     // enable automatic scrubbing
	NoScrub              bool   `json:"noScrub,omitempty"`       // disable scrubbing
	ScrubInterval        string `json:"scrubInterval,omitempty"` // e.g., "24h"
	ScrubQueueDepth      int    `json:"scrubQueueDepth,omitempty"`
	ScrubSleep           string `json:"scrubSleep,omitempty"` // duration per scrub batch
	ScrubListLimit       int    `json:"scrubListLimit,omitempty"`
	ScrubFindBest        bool   `json:"scrubFindBest,omitempty"`
	ScrubECMaxBruteforce int    `json:"scrubEcMaxBruteforce,omitempty"`

	RecoveryTuneInterval string `json:"recoveryTuneInterval,omitempty"`
	RecoveryTuneUtilLow  int    `json:"recoveryTuneUtilLow,omitempty"`  // % CPU below which tune down
	RecoveryTuneUtilHigh int    `json:"recoveryTuneUtilHigh,omitempty"` // % CPU above which tune up
}
type ClusterParameters struct{}

// VitastorClusterSpec defines the desired state of VitastorCluster
type VitastorClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	VitastorNodeLabel string
	Agent             AgentSpec         `json:"agent"`
	Monitor           MonitorSpec       `json:"monitor"`
	OSD               OSDSpec           `json:"osd"`
	ReconcilePeriod   int               `json:"reconcilePeriod"`
	ClusterParameters ClusterParameters `json:"cluster"`
}

// VitastorClusterStatus defines the observed state of VitastorCluster.
type VitastorClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the VitastorCluster resource.
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
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VitastorCluster is the Schema for the vitastorclusters API
type VitastorCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of VitastorCluster
	// +required
	Spec VitastorClusterSpec `json:"spec"`

	// status defines the observed state of VitastorCluster
	// +optional
	Status VitastorClusterStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// VitastorClusterList contains a list of VitastorCluster
type VitastorClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VitastorCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VitastorCluster{}, &VitastorClusterList{})
}
