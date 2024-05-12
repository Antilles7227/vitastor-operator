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
	"go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"

	controlv1 "gitlab.com/Antilles7227/vitastor-operator/api/v1"
)

// VitastorPoolReconciler reconciles a VitastorPool object
type VitastorPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type VitastorPoolConfig struct {
	Name               string `json:"name"`
	Scheme             string `json:"scheme"`
	PGSize             int32  `json:"pg_size"`
	ParityChunks       int32  `json:"parity_chunks,omitempty"`
	PGMinSize          int32  `json:"pg_minsize"`
	PGCount            int32  `json:"pg_count"`
	FailureDomain      string `json:"failure_domain,omitempty"`
	MaxOSDCombinations int32  `json:"max_osd_combinations,omitempty"`
	BlockSize          int32  `json:"block_size,omitempty"`
	ImmediateCommit    string `json:"immediate_commit,omitempty"`
	OSDTags            string `json:"osd_tags,omitempty"`
}

//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorpools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorpools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorpools/finalizers,verbs=update
//+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VitastorPool object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *VitastorPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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

	var vitastorPool controlv1.VitastorPool
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: req.Name}, &vitastorPool); err != nil {
		log.Error(err, "unable to fetch VitastorPool, skipping")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check pool tree
	log.Info("Checking pools config")
	poolsPath := config.VitastorPrefix + "/config/pools"
	poolsConfigRaw, err := cli.Get(ctx, poolsPath)
	if err != nil {
		log.Error(err, "Unable to retrive pools config")
		return ctrl.Result{}, err
	}
	var pools map[string]VitastorPoolConfig
	if poolsConfigRaw.Count != 0 {
		err = json.Unmarshal(poolsConfigRaw.Kvs[0].Value, &pools)
		if err != nil {
			log.Error(err, "Unable to parse pools config block")
			return ctrl.Result{}, err
		}
	} else {
		pools = make(map[string]VitastorPoolConfig)
	}
	pools[vitastorPool.Spec.ID] = VitastorPoolConfig{
		Name:          vitastorPool.Name,
		Scheme:        vitastorPool.Spec.Scheme,
		PGSize:        vitastorPool.Spec.PGSize,
		PGMinSize:     vitastorPool.Spec.PGMinSize,
		ParityChunks:  vitastorPool.Spec.ParityChunks,
		PGCount:       vitastorPool.Spec.PGCount,
		FailureDomain: vitastorPool.Spec.FailureDomain,
	}
	var poolsBytes []byte
	poolsBytes, err = json.Marshal(pools)
	if err != nil {
		log.Error(err, "Unable to marshal pools config block")
		return ctrl.Result{}, err
	}
	poolsResp, err := cli.Put(ctx, poolsPath, string(poolsBytes))
	if err != nil {
		log.Error(err, "Unable to update pools tree")
		return ctrl.Result{}, err
	}
	log.Info(poolsResp.Header.String())

	var storageClass storage.StorageClass
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: req.Name}, &storageClass); err != nil {
		if errors.IsNotFound(err) {
			// StorageClass for that pool not found - creating new one
			log.Info("StorageClass is not found, creating new one")
			sc, err := r.getStorageClassConfig(&vitastorPool, &config)
			if err != nil {
				log.Error(err, "Failed to create storage class for that pool")
				return ctrl.Result{}, err
			}
			if err := controllerutil.SetControllerReference(&vitastorPool, sc, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner for storageclass")
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, sc); err != nil {
				log.Error(err, "Failed to create StorageClass")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, err
		}
		log.Error(err, "Unable to get StorageClass")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *VitastorPoolReconciler) getStorageClassConfig(pool *controlv1.VitastorPool, config *VitastorConfig) (*storage.StorageClass, error) {

	storageClassParameters := map[string]string{
		"etcdVolumePrefix": "",
		"poolId":           pool.Spec.ID,
	}

	storageClass := storage.StorageClass{
		ObjectMeta: ctrl.ObjectMeta{
			Name: pool.Name,
		},
		Provisioner: "csi.vitastor.io",
		Parameters:  storageClassParameters,
	}
	return &storageClass, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VitastorPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlv1.VitastorPool{}).
		Owns(&storage.StorageClass{}).
		Complete(r)
}
