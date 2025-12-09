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
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	controlv1 "gitlab.com/Antilles7227/vitastor-operator/api/v1"
	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
)

// VitastorNodeReconciler reconciles a VitastorNode object
type VitastorNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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
	placementLevelRaw, err := cli.Get(ctx, nodePlacementPath)
	if err != nil {
		log.Error(err, "Unable to retrieve placement tree")
		return ctrl.Result{}, err
	}
	var placementLevel map[string]VitastorNodePlacement
	if placementLevelRaw.Count != 0 {
		err = json.Unmarshal(placementLevelRaw.Kvs[0].Value, &placementLevel)
		if err != nil {
			log.Error(err, "Unable to parse placement level block")
			return ctrl.Result{}, err
		}
	} else {
		placementLevel = make(map[string]VitastorNodePlacement)
	}
	placementLevel[vitastorNode.Name] = VitastorNodePlacement{Level: "host"}
	// Check if node has fd.vitastor.io labels
	// TODO: If there are more than 1 FD label that code will fail, need to refactor it to proper FD chaining
	for label, value := range k8sNode.Labels {
		if strings.Contains(label, "fd.vitastor.io") {
			splittedLabel := strings.Split(label, "/")
			if contains_str_list(placementLevelCluster, splittedLabel[1]) {
				// Node labeled properly, check if that label exist in placements
				_, ok := placementLevel[value]
				// If the key not exists
				if !ok {
					placementLevel[value] = VitastorNodePlacement{Level: splittedLabel[1]}
				}
				// Updating placement level with proper parent
				placementLevel[vitastorNode.Name] = VitastorNodePlacement{Level: "host", Parent: value}
			}
		}
	}

	var placementLevelBytes []byte
	placementLevelBytes, err = json.Marshal(placementLevel)
	if err != nil {
		log.Error(err, "Unable to marshal placement level block")
		return ctrl.Result{}, err
	}
	placementLevelResp, err := cli.Put(ctx, nodePlacementPath, string(placementLevelBytes))
	if err != nil {
		log.Error(err, "Unable to update placement level tree")
		return ctrl.Result{}, err
	}
	log.Info(placementLevelResp.Header.String())

	// Update status with OSDs
	agentList := &corev1.PodList{}
	getOpts := []client.ListOption{
		client.InNamespace(ownerCluster.Spec.VitastorClusterNamespace),
		client.MatchingLabels{"app": "vitastor-agent"},
		client.MatchingFields{".spec.node": vitastorNode.Name},
	}
	log.Info("Fetching agent for that VitastorNode...")
	if err := r.List(ctx, agentList, getOpts...); err != nil {
		log.Error(err, "unable to fetch agent for that VitastorNode CRD")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if len(agentList.Items) == 0 {
		log.Info("Seems like that agent Pod is not running, reschedule reconciling...")
		return ctrl.Result{RequeueAfter: time.Duration(ownerCluster.Spec.ReconcilePeriodMin) * time.Minute}, nil
	}
	agentIP := agentList.Items[0].Status.PodIP
	systemDisksURL := "http://" + agentIP + ":8000/disk"

	// Getting all disks on that node
	resp, err := http.Get(systemDisksURL)
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
	json.Unmarshal(body, &systemDisks)
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
			// Check node placement and set if empty
			placementLevelRaw, err := cli.Get(ctx, nodePlacementPath)
			if err != nil {
				log.Error(err, "Unable to retrieve placement tree")
				return ctrl.Result{}, err
			}
			var placementLevel map[string]VitastorNodePlacement
			err = json.Unmarshal(placementLevelRaw.Kvs[0].Value, &placementLevel)
			if err != nil {
				log.Error(err, "Unable to parse placement level block")
				return ctrl.Result{}, err
			}
			placementLevel[vitastorNode.Name+"_"+strings.Trim("/dev/", disk.Name)] = VitastorNodePlacement{Level: "disk", Parent: vitastorNode.Name}
			var placementLevelBytes []byte
			placementLevelBytes, err = json.Marshal(placementLevel)
			if err != nil {
				log.Error(err, "Unable to marshal placement level block")
				return ctrl.Result{}, err
			}
			placementLevelResp, err := cli.Put(ctx, nodePlacementPath, string(placementLevelBytes))
			if err != nil {
				log.Error(err, "Unable to update placement level tree")
				return ctrl.Result{}, err
			}
			log.Info(placementLevelResp.Header.String())
			continue
		} else {
			// Disk not deployed, need to create CRD
			new_disk := r.getDiskConfiguration(disk.Name, &vitastorNode)
			if err := controllerutil.SetControllerReference(&vitastorNode, new_disk, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner for osd")
				return ctrl.Result{}, err
			}
			log.Info("Deploying new Disk", "diskName", new_disk.Name)
			err := r.Create(ctx, new_disk)
			if err != nil {
				log.Error(err, "Failed to create new OSD")
				return ctrl.Result{}, err
			}

			// Check node placement and set if empty
			placementLevelRaw, err := cli.Get(ctx, nodePlacementPath)
			if err != nil {
				log.Error(err, "Unable to retrieve placement tree")
				return ctrl.Result{}, err
			}
			var placementLevel map[string]VitastorNodePlacement
			err = json.Unmarshal(placementLevelRaw.Kvs[0].Value, &placementLevel)
			if err != nil {
				log.Error(err, "Unable to parse placement level block")
				return ctrl.Result{}, err
			}
			placementLevel[new_disk.Name] = VitastorNodePlacement{Level: "disk", Parent: vitastorNode.Name}
			var placementLevelBytes []byte
			placementLevelBytes, err = json.Marshal(placementLevel)
			if err != nil {
				log.Error(err, "Unable to marshal placement level block")
				return ctrl.Result{}, err
			}
			placementLevelResp, err := cli.Put(ctx, nodePlacementPath, string(placementLevelBytes))
			if err != nil {
				log.Error(err, "Unable to update placement level tree")
				return ctrl.Result{}, err
			}
			log.Info(placementLevelResp.Header.String())
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
			placementLevelRaw, err := cli.Get(ctx, nodePlacementPath)
			if err != nil {
				log.Error(err, "Unable to retrieve placement tree")
				return ctrl.Result{}, err
			}
			var placementLevel map[string]VitastorNodePlacement
			err = json.Unmarshal(placementLevelRaw.Kvs[0].Value, &placementLevel)
			if err != nil {
				log.Error(err, "Unable to parse placement level block")
				return ctrl.Result{}, err
			}
			delete(placementLevel, disk.Name)
			var placementLevelBytes []byte
			placementLevelBytes, err = json.Marshal(placementLevel)
			if err != nil {
				log.Error(err, "Unable to marshal placement level block")
				return ctrl.Result{}, err
			}
			placementLevelResp, err := cli.Put(ctx, nodePlacementPath, string(placementLevelBytes))
			if err != nil {
				log.Error(err, "Unable to update placement level tree")
				return ctrl.Result{}, err
			}
			log.Info(placementLevelResp.Header.String())

			log.Info("Deleting Disk...", "diskName", disk.Name)

			err = r.Delete(ctx, &disk)
			if err != nil {
				log.Error(err, "Failed to delete disk")
				return ctrl.Result{RequeueAfter: time.Duration(ownerCluster.Spec.ReconcilePeriodMin) * time.Minute}, err
			}
		}
	}
	log.Info("Reconciling is done")
	return ctrl.Result{RequeueAfter: time.Duration(ownerCluster.Spec.ReconcilePeriodMin) * time.Minute}, nil
}

func compareArrays(x, y []string) bool {
	less := func(a, b string) bool { return a < b }
	return cmp.Equal(x, y, cmpopts.SortSlices(less))
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
			Name: node.Name + "_" + strings.Trim("/dev/", diskPath),
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

func (r *VitastorNodeReconciler) getConfiguration(osdPath string, osdNumber int, node *controlv1.VitastorNode) *controlv1.VitastorOSD {
	osd := &controlv1.VitastorOSD{
		ObjectMeta: ctrl.ObjectMeta{
			Name: "vitastor-osd-" + strconv.Itoa(osdNumber),
		},
		Spec: controlv1.VitastorOSDSpec{
			NodeName:  node.Spec.NodeName,
			OSDPath:   osdPath,
			OSDNumber: osdNumber,
			OSDImage:  node.Spec.OSDImage,
		},
	}
	return osd
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
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &controlv1.VitastorOSD{}, ".spec.nodeName", func(rawObj client.Object) []string {
		osd := rawObj.(*controlv1.VitastorOSD)
		return []string{osd.Spec.NodeName}
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
