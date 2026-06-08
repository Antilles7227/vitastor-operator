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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
)

// VitastorNodeReconciler reconciles a VitastorNode object
type VitastorNodeReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	HttpClient *http.Client
}

type SystemPartition struct {
	Name     string `json:"name"`
	FStype   string `json:"fstype,omitempty"`
	PartUUID string `json:"partuuid,omitempty"`
}

type SystemDisk struct {
	Name     string            `json:"name"`
	Type     string            `json:"type,omitempty"`
	Children []SystemPartition `json:"children,omitempty"`
}

type OSDPartition struct {
	DataDevice      string `json:"data_device"`
	OSDNumber       int    `json:"osd_num"`
	ImmediateCommit string `json:"immediate_commit"`
}

type VitastorConfig struct {
	VitastorEtcdUrls []string `json:"etcd_address"`
	VitastorPrefix   string   `json:"etcd_prefix"`
}

type VitastorNodePlacement struct {
	Level  string `json:"level,omitempty"`
	Parent string `json:"parent,omitempty"`
}

//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastornodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastornodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastornodes/finalizers,verbs=update
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorosds,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=v1,resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VitastorNode object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *VitastorNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var log = log.FromContext(ctx)
	// ===
	// Preparations, getting cluster CR, node CR, connect to Vitastor etcd, checking namespace for resources
	// ===
	var vitastorNode controlv2.VitastorNode
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: req.Name}, &vitastorNode); err != nil {
		log.Error(err, "unable to fetch VitastorNode, skipping")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var ownerCluster controlv2.VitastorCluster
	for _, ownerRef := range vitastorNode.OwnerReferences {
		if ownerRef.Kind == "VitastorCluster" && ownerRef.APIVersion == "control.vitastor.io/v2" {
			log.Info("found owner VitastorCluster", "APIVersion", ownerRef.APIVersion, "name", ownerRef.Name)
			if err := r.Client.Get(ctx, types.NamespacedName{Name: ownerRef.Name, Namespace: corev1.NamespaceAll}, &ownerCluster); err != nil {
				log.Error(err, "unable to fetch owner VitastorCluster")
				return ctrl.Result{}, err
			}
			break
		}
	}
	config, err := loadConfiguration(ctx, "/etc/vitastor/vitastor.conf")
	if err != nil {
		log.Error(err, "Unable to load vitastor.conf")
		return ctrl.Result{}, err
	}
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   config.VitastorEtcdUrls,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Error(err, "Unable to connect to etcd")
		return ctrl.Result{}, err
	}
	defer cli.Close()

	// Propagate node-level noout to all owned OSDs
	if err := r.reconcileNodeNoOut(ctx, &vitastorNode); err != nil {
		log.Error(err, "Failed to reconcile node noout")
		// Non-fatal, continue
	}

	placementLevelCluster := make([]string, 0, len(ownerCluster.Spec.ClusterParameters.PlacementLevels))
	for k := range ownerCluster.Spec.ClusterParameters.PlacementLevels {
		placementLevelCluster = append(placementLevelCluster, k)
	}

	var k8sNode corev1.Node
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: req.Name}, &k8sNode); err != nil {
		log.Error(err, "unable to fetch Node, skipping")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check node placement and set if empty
	log.Info("Checking node_placement config")
	nodePlacementPath := config.VitastorPrefix + "/config/node_placement"
	// Determine parent from fd.vitastor.io labels
	// TODO: If there are more than 1 FD label that code will fail, need to refactor it to proper FD chaining
	parentValue := ""
	for label, value := range k8sNode.Labels {
		if strings.Contains(label, "fd.vitastor.io") {
			splittedLabel := strings.Split(label, "/")
			if contains_str_list(placementLevelCluster, splittedLabel[1]) {
				// Add FD label value as a placement level
				if err := r.updatePlacementEntry(ctx, cli, nodePlacementPath, value, VitastorNodePlacement{Level: splittedLabel[1]}); err != nil {
					log.Error(err, "Failed to update FD placement entry")
					return ctrl.Result{}, err
				}
				parentValue = value
			}
		}
	}
	if err := r.updatePlacementEntry(ctx, cli, nodePlacementPath, vitastorNode.Name, VitastorNodePlacement{Level: "host", Parent: parentValue}); err != nil {
		log.Error(err, "Failed to update node placement entry")
		return ctrl.Result{}, err
	}

	// Update status with OSDs
	agentList := &corev1.PodList{}
	getOpts := []client.ListOption{
		client.InNamespace(ownerCluster.Spec.VitastorClusterNamespace),
		client.MatchingLabels{"control.vitastor.io/app": "vitastor-agent"},
		client.MatchingFields{".spec.node": vitastorNode.Name},
	}
	log.Info("Fetching agent for that VitastorNode...")
	if err := r.List(ctx, agentList, getOpts...); err != nil {
		log.Error(err, "unable to fetch agent for that VitastorNode CRD")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if len(agentList.Items) == 0 || agentList.Items[0].Status.PodIP == "" {
		log.Info("Seems like that agent Pod is not running, reschedule reconciling...")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	agentIP := agentList.Items[0].Status.PodIP
	systemDisksURL := "http://" + agentIP + ":8000/disk"

	// Getting all disks on that node
	if r.HttpClient == nil {
		r.HttpClient = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := r.HttpClient.Get(systemDisksURL)
	if err != nil {
		log.Error(err, "Unable to get system disks")
		return ctrl.Result{}, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "Unable to read body of response")
		return ctrl.Result{}, err
	}
	var systemDisks []SystemDisk
	if err := json.Unmarshal(body, &systemDisks); err != nil {
		log.Error(err, "Unable to parse agent disk response")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	resp.Body.Close()
	var systemDisksPaths []string = make([]string, len(systemDisks))
	for _, disk := range systemDisks {
		systemDisksPaths = append(systemDisksPaths, disk.Name)
	}

	log.Info("Fetching Disks for that Node")
	diskList := &controlv2.VitastorDiskList{}
	listOpts := []client.ListOption{
		client.MatchingFields{".spec.nodeRef": vitastorNode.Name},
	}
	if err := r.List(ctx, diskList, listOpts...); err != nil {
		log.Error(err, "Unable to list disks")
		return ctrl.Result{}, err
	}

	// Checking existing VitastorDisk for creating new Disk CRs
	log.Info("Checking existing VitastorDisk for creating new Disk CRs")
	for _, disk := range systemDisks {
		if contains(diskList.Items, disk.Name) {
			// That disk already working in cluster, updating node placement and skip
			diskPlacementKey := vitastorNode.Name + "_" + strings.TrimPrefix(disk.Name, "/dev/")
			if err := r.updatePlacementEntry(ctx, cli, nodePlacementPath, diskPlacementKey, VitastorNodePlacement{Level: "disk", Parent: vitastorNode.Name}); err != nil {
				log.Error(err, "Failed to update disk placement entry")
				return ctrl.Result{}, err
			}
			continue
		} else {
			// Disk not deployed, need to create CRD
			new_disk := r.getDiskConfiguration(disk.Name, &vitastorNode)
			if err := controllerutil.SetControllerReference(&vitastorNode, new_disk, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner for osd")
				return ctrl.Result{}, err
			}
			log.Info("Deploying new Disk", "diskName", new_disk.Name)
			if err := r.Create(ctx, new_disk); err != nil {
				log.Error(err, "Failed to create new OSD")
				return ctrl.Result{}, err
			}

			if err := r.updatePlacementEntry(ctx, cli, nodePlacementPath, new_disk.Name, VitastorNodePlacement{Level: "disk", Parent: vitastorNode.Name}); err != nil {
				log.Error(err, "Failed to update disk placement entry")
				return ctrl.Result{}, err
			}
		}
	}

	// Checking existing VitastorDisks for deleting disabled Disks
	log.Info("Checking existing VitastorDisks for deleting disabled Disks")
	for _, disk := range diskList.Items {
		if contains_str_list(systemDisksPaths, disk.Spec.DevicePath) {
			// That disk still working in cluster, skip
			continue
		} else {
			// Disk disappeared, deleting CR and updating node placement
			if err := r.deletePlacementEntry(ctx, cli, nodePlacementPath, disk.Name); err != nil {
				log.Error(err, "Failed to delete disk placement entry")
				return ctrl.Result{}, err
			}

			log.Info("Deleting Disk...", "diskName", disk.Name)

			if err := r.Delete(ctx, &disk); err != nil {
				log.Error(err, "Failed to delete disk")
				return ctrl.Result{RequeueAfter: time.Duration(ownerCluster.Spec.ReconcilePeriodMin) * time.Minute}, err
			}
		}
	}
	log.Info("Reconciling is done")
	return ctrl.Result{RequeueAfter: time.Duration(ownerCluster.Spec.ReconcilePeriodMin) * time.Minute}, nil
}

// updatePlacementEntry atomically reads the node_placement map from etcd,
// applies the given modification, and writes it back.
func (r *VitastorNodeReconciler) updatePlacementEntry(ctx context.Context, cli *clientv3.Client, path string, key string, placement VitastorNodePlacement) error {
	log := log.FromContext(ctx)

	resp, err := cli.Get(ctx, path)
	if err != nil {
		return fmt.Errorf("unable to retrieve placement tree: %w", err)
	}

	var placementLevel map[string]VitastorNodePlacement
	if resp.Count != 0 {
		if err := json.Unmarshal(resp.Kvs[0].Value, &placementLevel); err != nil {
			return fmt.Errorf("unable to parse placement level block: %w", err)
		}
	} else {
		placementLevel = make(map[string]VitastorNodePlacement)
	}

	placementLevel[key] = placement

	placementLevelBytes, err := json.Marshal(placementLevel)
	if err != nil {
		return fmt.Errorf("unable to marshal placement level block: %w", err)
	}

	putResp, err := cli.Put(ctx, path, string(placementLevelBytes))
	if err != nil {
		return fmt.Errorf("unable to update placement level tree: %w", err)
	}
	log.Info("Updated placement level", "key", key, "revision", putResp.Header.Revision)
	return nil
}

// deletePlacementEntry atomically reads the node_placement map from etcd,
// removes the given key, and writes it back.
func (r *VitastorNodeReconciler) deletePlacementEntry(ctx context.Context, cli *clientv3.Client, path string, key string) error {
	log := log.FromContext(ctx)

	resp, err := cli.Get(ctx, path)
	if err != nil {
		return fmt.Errorf("unable to retrieve placement tree: %w", err)
	}
	if resp.Count == 0 {
		return nil // nothing to delete
	}

	var placementLevel map[string]VitastorNodePlacement
	if err := json.Unmarshal(resp.Kvs[0].Value, &placementLevel); err != nil {
		return fmt.Errorf("unable to parse placement level block: %w", err)
	}

	if _, exists := placementLevel[key]; !exists {
		return nil // already not present
	}

	delete(placementLevel, key)

	placementLevelBytes, err := json.Marshal(placementLevel)
	if err != nil {
		return fmt.Errorf("unable to marshal placement level block: %w", err)
	}

	putResp, err := cli.Put(ctx, path, string(placementLevelBytes))
	if err != nil {
		return fmt.Errorf("unable to update placement level tree: %w", err)
	}
	log.Info("Removed placement entry", "key", key, "revision", putResp.Header.Revision)
	return nil
}

func contains(diskList []controlv2.VitastorDisk, diskName string) bool {
	for _, v := range diskList {
		if v.Spec.DevicePath == diskName {
			return true
		}
	}
	return false
}

func contains_str_list(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

func (r *VitastorNodeReconciler) getDiskConfiguration(diskPath string, node *controlv2.VitastorNode) *controlv2.VitastorDisk {
	disk := &controlv2.VitastorDisk{
		ObjectMeta: ctrl.ObjectMeta{
			Name: node.Name + "_" + strings.TrimPrefix(diskPath, "/dev/"),
			Labels: map[string]string{
				"control.vitastor.io/cluster": node.Labels["control.vitastor.io/cluster"],
				"control.vitastor.io/node":    node.Name,
			},
		},
		Spec: controlv2.VitastorDiskSpec{
			NodeRef:    node.Name,
			DevicePath: diskPath,
		},
	}
	return disk
}

func (r *VitastorNodeReconciler) reconcileNodeNoOut(ctx context.Context, vitastorNode *controlv2.VitastorNode) error {
	log := log.FromContext(ctx)

	// List all OSDs owned by this node
	osdList := &controlv2.VitastorOSDList{}
	if err := r.List(ctx, osdList, client.MatchingLabels{
		"control.vitastor.io/node": vitastorNode.Name,
	}); err != nil {
		return fmt.Errorf("listing OSDs for node %s: %w", vitastorNode.Name, err)
	}

	for _, osd := range osdList.Items {
		if osd.Spec.NoOut != vitastorNode.Spec.NoOut {
			osd.Spec.NoOut = vitastorNode.Spec.NoOut
			if err := r.Update(ctx, &osd); err != nil {
				log.Error(err, "Failed to update OSD noout", "osd", osd.Name)
				return err
			}
			log.Info("Propagated node noout to OSD", "node", vitastorNode.Name, "osd", osd.Name, "noout", vitastorNode.Spec.NoOut)
		}
	}
	return nil
}

func loadConfiguration(ctx context.Context, file string) (VitastorConfig, error) {
	log := log.FromContext(ctx)
	var config VitastorConfig
	configFile, err := os.Open(file)
	if err != nil {
		log.Error(err, "Unable to open config")
		return VitastorConfig{}, err
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	jsonParser.Decode(&config)
	return config, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VitastorNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &controlv2.VitastorDisk{}, ".spec.nodeRef", func(rawObj client.Object) []string {
		osd := rawObj.(*controlv2.VitastorDisk)
		return []string{osd.Spec.NodeRef}
	}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, ".spec.node", func(rawObj client.Object) []string {
		agent := rawObj.(*corev1.Pod)
		return []string{agent.Spec.NodeName}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&controlv2.VitastorNode{}).
		Owns(&controlv2.VitastorDisk{}).
		Complete(r)
}
