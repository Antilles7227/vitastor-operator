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
	Image                 string                      `json:"image"`
	Replicas              int32                       `json:"replicas"`
	Resources             corev1.ResourceRequirements `json:"resources,omitempty"`
	EtcdMonTtlSecs        int32                       `json:"etcdMonTtlSecs,omitempty"`
	EtcdMonTimeoutMsecs   int32                       `json:"etcdMonTimeoutMsecs,omitempty"`
	EtcdMonRetries        int32                       `json:"etcdMonRetries,omitempty"`
	MonChangeTimeoutMsecs int32                       `json:"monChangeTimeoutMsecs,omitempty"`
	MonStatsTimeoutMsecs  int32                       `json:"monStatsTimeoutMsecs,omitempty"`
	OsdOutTimeSecs        int32                       `json:"osdOutTimeSecs,omitempty"`
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
	RecoveryPGSwitch       int32                       `json:"recoveryPgSwitch,omitempty"`
	RecoverySyncBatch      int32                       `json:"recoverySyncBatch,omitempty"`
	NoRecovery             bool                        `json:"noRecovery,omitempty"`
	NoRebalance            bool                        `json:"noRebalance,omitempty"`

	PrintStatsIntervalSecs int32 `json:"printStatsIntervalSecs,omitempty"`
	SlowLogIntervalSecs    int32 `json:"slowLogIntervalSecs,omitempty"`

	AutoScrub            bool   `json:"autoScrub,omitempty"`
	NoScrub              bool   `json:"noScrub,omitempty"`
	ScrubInterval        string `json:"scrubInterval,omitempty"`
	ScrubQueueDepth      int32  `json:"scrubQueueDepth,omitempty"`
	ScrubSleepMsec       int32  `json:"scrubSleepMsec,omitempty"`
	ScrubListLimit       int    `json:"scrubListLimit,omitempty"`
	ScrubFindBest        bool   `json:"scrubFindBest,omitempty"`
	ScrubECMaxBruteforce int32  `json:"scrubEcMaxBruteforce,omitempty"`

	RecoveryTuneIntervalSecs int32  `json:"recoveryTuneIntervalSecs,omitempty"`
	RecoveryTuneUtilLow      string `json:"recoveryTuneUtilLow,omitempty"`
	RecoveryTuneUtilHigh     string `json:"recoveryTuneUtilHigh,omitempty"`

	DisableMetaFsync    bool `json:"disableMetaFsync"`
	DisableJournalFsync bool `json:"disableJournalFsync"`
	DisableDataFsync    bool `json:"disableDataFsync"`
}
type ClusterParameters struct {
	OsdBackfillRatio string           `json:"osdBackfillRatio,omitempty"`
	PlacementLevels  map[string]int32 `json:"placementLevels,omitempty"`
	ImmediateCommit  ImmediateCommit  `json:"immediateCommit,omitempty"`
}

// +kubebuilder:validation:Enum=none;small;all
type ImmediateCommit string

// VitastorClusterSpec defines the desired state of VitastorCluster
type VitastorClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	VitastorNodeLabel        string            `json:"vitastorNodeLabel"`
	VitastorClusterNamespace string            `json:"vitastorClusterNamespace,omitempty"`
	Agent                    AgentSpec         `json:"agent"`
	Monitor                  MonitorSpec       `json:"monitor"`
	OSD                      OSDSpec           `json:"osd"`
	ReconcilePeriodMin       int               `json:"reconcilePeriodMin"`
	ClusterParameters        ClusterParameters `json:"cluster"`
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
	// Represents OSD that now is allowed to update
	ActiveOSD  string             `json:"activeRollingOSD,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Cluster

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
