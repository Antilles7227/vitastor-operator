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

package controllers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
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
	namespace, isEmpty := os.LookupEnv("NAMESPACE")
	if !isEmpty {
		namespace = "vitastor-system"
	}
	updateIntervalRaw, isEmpty := os.LookupEnv("UPDATE_INTERVAL")
	if !isEmpty {
		updateIntervalRaw = "15"
	}
	updateInterval, err := strconv.Atoi(updateIntervalRaw)
	if err != nil {
		return ctrl.Result{}, err
	}

	var vitastorNode controlv1.VitastorNode
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: req.Name}, &vitastorNode); err != nil {
		log.Error(err, "unable to fetch VitastorNode, skipping")
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
	var nodeStatusUpdated bool = false
	agentList := &corev1.PodList{}
	getOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{"app": "vitastor-agent"},
		client.MatchingFields{".spec.node": vitastorNode.Name},
	}
	log.Info("Fetching agent for that VitastorNode...")
	if err := r.List(ctx, agentList, getOpts...); err != nil {
		log.Error(err, "unable to fetch agent for that VitastorNode CRD")
		log.Error(err, "Unable to update status of node")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	agentIP := agentList.Items[0].Status.PodIP
	systemDisksURL := "http://" + agentIP + ":8000/disk"
	emptyDisksURL := systemDisksURL + "/empty"
	osdURL := systemDisksURL + "/osd"

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
	if !compareArrays(systemDisksPaths, vitastorNode.Status.Disks) {
		log.Info("Status of system disks is not actual, updating")
		vitastorNode.Status.Disks = systemDisksPaths
		nodeStatusUpdated = true
	}

	// Getting empty disks on that node
	resp, err = http.Get(emptyDisksURL)
	if err != nil {
		log.Error(err, "Unable to get empty disks")
		return ctrl.Result{}, err
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "Unable to read body of response")
		return ctrl.Result{}, err
	}
	var emptyDisks []SystemDisk
	json.Unmarshal(body, &emptyDisks)
	resp.Body.Close()
	var emptyDisksPaths []string = make([]string, len(emptyDisks))
	for _, disk := range emptyDisks {
		emptyDisksPaths = append(emptyDisksPaths, disk.Name)
	}
	if !compareArrays(emptyDisksPaths, vitastorNode.Status.EmptyDisks) {
		log.Info("Status of empty disks is not actual, updating")
		vitastorNode.Status.EmptyDisks = emptyDisksPaths
		nodeStatusUpdated = true
	}

	// Getting OSD on that node
	resp, err = http.Get(osdURL)
	if err != nil {
		log.Error(err, "Unable to get OSDs")
		return ctrl.Result{}, err
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "Unable to read body of response")
		return ctrl.Result{}, err
	}
	var osds []OSDPartition
	json.Unmarshal(body, &osds)
	resp.Body.Close()
	var osdPaths []string = make([]string, len(osds))
	for _, disk := range osds {
		osdPaths = append(osdPaths, disk.DataDevice)
	}
	if !compareArrays(osdPaths, vitastorNode.Status.VitastorDisks) {
		log.Info("Status of OSDs is not actual, updating")
		vitastorNode.Status.VitastorDisks = osdPaths
		nodeStatusUpdated = true
	}

	if nodeStatusUpdated {
		log.Info("Updating node status")
		err := r.Status().Update(ctx, &vitastorNode)
		if err != nil {
			log.Error(err, "Unable to update status of vitastorNode")
			return ctrl.Result{}, err
		}
	}

	log.Info("Fetching OSDs for that Node")
	osdList := &controlv1.VitastorOSDList{}
	listOpts := []client.ListOption{
		client.MatchingFields{".spec.nodeName": vitastorNode.Name},
	}
	if err := r.List(ctx, osdList, listOpts...); err != nil {
		log.Error(err, "Unable to list OSDs")
		return ctrl.Result{}, err
	}

	// Checking existing VitastorOSD for creating new OSDs
	log.Info("Checking existing VitastorOSD for creating new OSDs")
	for _, osd := range osds {
		if contains(osdList.Items, osd.DataDevice) {
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
			placementLevel[strconv.Itoa(osd.OSDNumber)] = VitastorNodePlacement{Level: "osd", Parent: vitastorNode.Spec.NodeName}
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
			// OSD for that disk not deployed, need to create CRD
			new_osd := r.getConfiguration(osd.DataDevice, osd.OSDNumber, &vitastorNode)
			if err := controllerutil.SetControllerReference(&vitastorNode, new_osd, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner for osd")
				return ctrl.Result{}, err
			}
			log.Info("Deploying new OSD", "osdName", new_osd.Name)
			err := r.Create(ctx, new_osd)
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
			placementLevel[strconv.Itoa(new_osd.Spec.OSDNumber)] = VitastorNodePlacement{Level: "osd", Parent: vitastorNode.Spec.NodeName}
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

	// Checking existing VitastorOSD for deleting disabled OSD
	log.Info("Checking existing VitastorOSD for deleting disabled OSDs")
	for _, osd := range osdList.Items {
		if contains_str_list(osdPaths, osd.Spec.OSDPath) {
			// That disk still working in cluster, checking image
			if osd.Spec.OSDImage != vitastorNode.Spec.OSDImage {
				log.Info("Updating OSD image", "osdName", osd.Name, "oldImage", osd.Spec.OSDImage, "newImage", vitastorNode.Spec.OSDImage)
				osd.Spec.OSDImage = vitastorNode.Spec.OSDImage
				if err := r.Update(ctx, &osd); err != nil {
					log.Error(err, "Failed to update OSD object")
					return ctrl.Result{}, err
				}
			}
			continue
		} else {
			// OSD for that disk disappeared, deleting OSD
			log.Info("Deleting OSD...", "osdName", osd.Name)
			err := r.Delete(ctx, &osd)
			if err != nil {
				log.Error(err, "Failed to delete OSD")
				return ctrl.Result{}, err
			}
		}
	}
	log.Info("Reconciling is done")
	return ctrl.Result{RequeueAfter: time.Duration(updateInterval) * time.Minute}, nil
}

func compareArrays(x, y []string) bool {
	less := func(a, b string) bool { return a < b }
	return cmp.Equal(x, y, cmpopts.SortSlices(less))
}

func contains(s []controlv1.VitastorOSD, str string) bool {
	for _, v := range s {
		if v.Spec.OSDPath == str {
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

func (r *VitastorNodeReconciler) getConfiguration(osdPath string, osdNumber int, node *controlv1.VitastorNode) *controlv1.VitastorOSD {
	osd := &controlv1.VitastorOSD{
		ObjectMeta: ctrl.ObjectMeta{
			Name: "vitastor-osd-" + strconv.Itoa(osdNumber),
		},
		Spec: controlv1.VitastorOSDSpec{
			NodeName:  node.Spec.NodeName,
			OSDPath:   osdPath,
			OSDNumber: osdNumber,
			OSDImage: node.Spec.OSDImage,
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
		For(&controlv1.VitastorNode{}).
		Owns(&controlv1.VitastorOSD{}).
		Complete(r)
}
