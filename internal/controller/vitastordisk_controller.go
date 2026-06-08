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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
)

// DiskStatus* constants reflect the controller-internal observed state of a VitastorDisk.
const (
	DiskStatusEmpty          = "Empty"
	DiskStatusOSD            = "OSD"
	DiskStatusDraining       = "Draining"
	DiskStatusDecommissioned = "Decommissioned"
)

// VitastorDiskReconciler reconciles a VitastorDisk object
type VitastorDiskReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	HttpClient *http.Client
}

type OSDPrepareParameters struct {
	Disk   string `json:"disk"`
	OSDNum *int   `json:"osd_num,omitempty"`
}

type VitastorParameters struct {
	DataDevice string `json:"data_device"`
	OSDNum     int    `json:"osd_num"`
	// ... остальные поля, если нужны
}

// +kubebuilder:rbac:groups=control.vitastor.io,resources=vitastordisks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=control.vitastor.io,resources=vitastordisks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=control.vitastor.io,resources=vitastordisks/finalizers,verbs=update
// +kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorosds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *VitastorDiskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var disk controlv2.VitastorDisk
	if err := r.Get(ctx, req.NamespacedName, &disk); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	namespace, err := r.getClusterNamespace(ctx, &disk)
	if err != nil {
		log.Error(err, "Failed to determine cluster namespace")
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	agentPod, err := r.findAgentPod(ctx, disk.Spec.NodeRef, namespace)
	if err != nil {
		log.Error(err, "Failed to find agent pod for node", "node", disk.Spec.NodeRef)
		// Retry slower, maybe node is down or agent restarting
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	foundOSDs, err := r.checkDiskState(agentPod.Status.PodIP, disk.Spec.DevicePath)
	if err != nil {
		log.Error(err, "Failed to check disk state via agent")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	isOSD := len(foundOSDs) > 0

	currentStatusType := controlv2.DiskType(DiskStatusEmpty)
	currentStatusState := DiskStatusEmpty

	if isOSD {
		currentStatusType = controlv2.DiskType(DiskStatusOSD)
		currentStatusState = DiskStatusOSD
	}

	// Only update the observed status when we are not in a terminal drain/decommission
	// state — otherwise we would overwrite "Draining" back to "OSD".
	statusChanged := false
	if disk.Status.State != DiskStatusDraining && disk.Status.State != DiskStatusDecommissioned {
		if disk.Status.Type != currentStatusType || disk.Status.State != currentStatusState {
			disk.Status.Type = currentStatusType
			disk.Status.State = currentStatusState
			statusChanged = true
		}
	}

	if statusChanged {
		if err := r.Status().Update(ctx, &disk); err != nil {
			return ctrl.Result{}, err
		}
		// Re-fetch to avoid conflict in subsequent update
		return ctrl.Result{Requeue: true}, nil
	}

	switch disk.Spec.DesiredState {
	case controlv2.DiskStatePrepared:
		if isOSD {
			for _, param := range foundOSDs {
				if err := r.ensureOSDCR(ctx, &disk, &param, namespace); err != nil {
					log.Error(err, "Failed to ensure VitastorOSD CR exists", "id", param.OSDNum)
					return ctrl.Result{}, err
				}
			}
		} else {
			log.Info("Provisioning OSD on disk", "disk", disk.Spec.DevicePath)
			newParams, err := r.provisionOSD(agentPod.Status.PodIP, disk.Spec.DevicePath, disk.Spec.DesiredOSDCount)
			if err != nil {
				log.Error(err, "Provisioning failed")
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
			}
			if len(newParams) == 0 {
				log.Error(nil, "Provisioning returned empty list")
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
			}
			for _, param := range newParams {
				if err := r.ensureOSDCR(ctx, &disk, &param, namespace); err != nil {
					log.Error(err, "Failed to ensure OSD CR", "osd_id", param.OSDNum)
					return ctrl.Result{}, err
				}
			}
			disk.Status.State = DiskStatusOSD
			disk.Status.Type = controlv2.DiskType(DiskStatusOSD)
			r.Status().Update(ctx, &disk)
		}

	case controlv2.DiskStateDecommissioned:
		// Phase B: if we are already draining, check whether drain is complete.
		if disk.Status.State == DiskStatusDraining {
			log.Info("Checking OSD drain status", "disk", disk.Name)
			drained, err := r.areOSDsDrained(ctx, &disk, namespace)
			if err != nil {
				log.Error(err, "Failed to check OSD drain status")
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
			}
			if !drained {
				log.Info("OSDs not yet drained, requeuing", "disk", disk.Name)
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
			}
			log.Info("OSDs drained, removing OSD CRs", "disk", disk.Name)
			if err := r.deleteOSDCR(ctx, &disk, namespace); err != nil {
				return ctrl.Result{}, err
			}
			disk.Status.State = DiskStatusDecommissioned
			if err := r.Status().Update(ctx, &disk); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}

		// Phase A: initiate drain — set all owned OSD weights to 0.
		if isOSD && disk.Status.State != DiskStatusDecommissioned {
			log.Info("Initiating disk decommission: setting OSD weights to 0", "disk", disk.Name)
			if err := r.initiateOSDDrain(ctx, &disk, namespace); err != nil {
				log.Error(err, "Failed to initiate OSD drain")
				return ctrl.Result{}, err
			}
			disk.Status.State = DiskStatusDraining
			if err := r.Status().Update(ctx, &disk); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
		}

	case controlv2.DiskStateDiscovered:
		// Do nothing, just observation
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *VitastorDiskReconciler) findAgentPod(ctx context.Context, nodeName, namespace string) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	opts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{"control.vitastor.io/app": "vitastor-agent"},
		client.MatchingFields{".spec.nodeName": nodeName},
	}
	if err := r.List(ctx, podList, opts...); err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("agent pod not found")
	}
	pod := &podList.Items[0]
	if pod.Status.PodIP == "" {
		return nil, fmt.Errorf("agent pod has no IP")
	}
	return pod, nil
}

// checkDiskState calls GET /disk/osd and checks if our device is there
func (r *VitastorDiskReconciler) checkDiskState(agentIP, devicePath string) ([]VitastorParameters, error) {
	if r.HttpClient == nil {
		r.HttpClient = &http.Client{Timeout: 5 * time.Second}
	}

	resp, err := r.HttpClient.Get(fmt.Sprintf("http://%s:8000/disk/osd", agentIP))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var allOsds []VitastorParameters
	if err := json.NewDecoder(resp.Body).Decode(&allOsds); err != nil {
		return nil, nil
	}

	var foundOnThisDisk []VitastorParameters

	for _, osd := range allOsds {
		if strings.HasPrefix(osd.DataDevice, devicePath) {
			foundOnThisDisk = append(foundOnThisDisk, osd)
		}
	}

	return foundOnThisDisk, nil
}

// provisionOSD calls POST /disk/prepare
func (r *VitastorDiskReconciler) provisionOSD(agentIP, devicePath string, osdNum int32) ([]VitastorParameters, error) {
	if r.HttpClient == nil {
		r.HttpClient = &http.Client{Timeout: 30 * time.Second}
	}

	payload := OSDPrepareParameters{
		Disk: devicePath,
	}
	if osdNum > 0 {
		num := int(osdNum)
		payload.OSDNum = &num
	}

	body, _ := json.Marshal(payload)
	resp, err := r.HttpClient.Post(fmt.Sprintf("http://%s:8000/disk/prepare", agentIP), "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent returned status %d", resp.StatusCode)
	}

	var newOSDs []VitastorParameters
	if err := json.NewDecoder(resp.Body).Decode(&newOSDs); err != nil {
		return nil, err
	}
	return newOSDs, nil
}

// ensureOSDCR creates VitastorOSD CR if missing
func (r *VitastorDiskReconciler) ensureOSDCR(ctx context.Context, disk *controlv2.VitastorDisk, params *VitastorParameters, namespace string) error {
	osdName := fmt.Sprintf("vitastor-osd-%d", params.OSDNum)

	osd := &controlv2.VitastorOSD{}
	err := r.Get(ctx, types.NamespacedName{Name: osdName, Namespace: namespace}, osd)
	if err != nil && errors.IsNotFound(err) {
		// Create
		newOSD := &controlv2.VitastorOSD{
			ObjectMeta: metav1.ObjectMeta{
				Name:      osdName,
				Namespace: namespace,
				Labels: map[string]string{
					"vitastor.io/cluster": disk.Labels["vitastor.io/cluster"],
					"vitastor.io/disk":    disk.Name,
				},
			},
			Spec: controlv2.VitastorOSDSpec{
				Id:     int32(params.OSDNum),
				Path:   params.DataDevice,
				Weight: "1",
			},
		}
		// Устанавливаем Disk как Owner, чтобы при удалении диска удалялся OSD
		if err := controllerutil.SetControllerReference(disk, newOSD, r.Scheme); err != nil {
			return err
		}

		logf.FromContext(ctx).Info("Creating VitastorOSD CR", "name", osdName)
		return r.Create(ctx, newOSD)
	}
	return err
}

func (r *VitastorDiskReconciler) deleteOSDCR(ctx context.Context, disk *controlv2.VitastorDisk, namespace string) error {
	// Ищем OSD, принадлежащие этому диску
	osdList := &controlv2.VitastorOSDList{}
	if err := r.List(ctx, osdList, client.InNamespace(namespace), client.MatchingLabels{"vitastor.io/disk": disk.Name}); err != nil {
		return err
	}

	for _, osd := range osdList.Items {
		logf.FromContext(ctx).Info("Deleting VitastorOSD", "name", osd.Name)
		if err := r.Delete(ctx, &osd); err != nil {
			return client.IgnoreNotFound(err)
		}
	}
	return nil
}

// initiateOSDDrain sets the weight of all OSDs belonging to this disk to "0",
// which signals the Vitastor monitor to migrate data away from them.
func (r *VitastorDiskReconciler) initiateOSDDrain(ctx context.Context, disk *controlv2.VitastorDisk, namespace string) error {
	log := logf.FromContext(ctx)

	osdList := &controlv2.VitastorOSDList{}
	if err := r.List(ctx, osdList, client.InNamespace(namespace), client.MatchingLabels{
		"vitastor.io/disk": disk.Name,
	}); err != nil {
		return fmt.Errorf("listing OSDs for disk %s: %w", disk.Name, err)
	}

	for i := range osdList.Items {
		osd := &osdList.Items[i]
		if osd.Spec.Weight != "0" {
			osd.Spec.Weight = "0"
			if err := r.Update(ctx, osd); err != nil {
				log.Error(err, "Failed to set OSD weight to 0", "osd", osd.Name)
				return err
			}
			log.Info("Set OSD weight to 0 for drain", "osd", osd.Name)
		}
	}
	return nil
}

// areOSDsDrained returns true when all OSDs belonging to the disk have weight "0"
// and are no longer in the Running state (i.e., data migration is complete).
func (r *VitastorDiskReconciler) areOSDsDrained(ctx context.Context, disk *controlv2.VitastorDisk, namespace string) (bool, error) {
	osdList := &controlv2.VitastorOSDList{}
	if err := r.List(ctx, osdList, client.InNamespace(namespace), client.MatchingLabels{
		"vitastor.io/disk": disk.Name,
	}); err != nil {
		return false, fmt.Errorf("listing OSDs for disk %s: %w", disk.Name, err)
	}

	// If no OSD CRs exist, drain is complete
	if len(osdList.Items) == 0 {
		return true, nil
	}

	// Check that all OSDs have weight=0 and are not in Running state
	for _, osd := range osdList.Items {
		if osd.Spec.Weight != "0" {
			return false, nil // weight not yet set to 0
		}
		if osd.Status.State == OSDStateRunning {
			return false, nil // still running
		}
	}

	// All OSDs have weight=0 and are not running — consider drained
	return true, nil
}

func (r *VitastorDiskReconciler) getClusterNamespace(ctx context.Context, disk *controlv2.VitastorDisk) (string, error) {
	clusterName, ok := disk.Labels["control.vitastor.io/cluster"]
	if !ok || clusterName == "" {
		return "vitastor-system", nil // fallback default
	}
	var cluster controlv2.VitastorCluster
	if err := r.Get(ctx, types.NamespacedName{Name: clusterName}, &cluster); err != nil {
		return "vitastor-system", nil // fallback
	}
	if cluster.Spec.VitastorClusterNamespace != "" {
		return cluster.Spec.VitastorClusterNamespace, nil
	}
	return "vitastor-system", nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VitastorDiskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, ".spec.nodeName", func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&controlv2.VitastorDisk{}).
		Owns(&controlv2.VitastorOSD{}).
		Complete(r)
}
