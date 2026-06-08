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
	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"
	"time"

	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
)

// VitastorPoolReconciler reconciles a VitastorPool object
type VitastorPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type VitastorPoolConfig struct {
	Name                string  `json:"name"`
	Scheme              string  `json:"scheme"`
	PGSize              int32   `json:"pg_size"`
	ParityChunks        *int32  `json:"parity_chunks,omitempty"`
	PGMinSize           int32   `json:"pg_minsize"`
	PGCount             int32   `json:"pg_count"`
	FailureDomain       *string `json:"failure_domain,omitempty"`
	LevelPlacement      *string `json:"level_placement,omitempty"`
	RawPlacement        *string `json:"raw_placement,omitempty"`
	LocalReads          *string `json:"local_reads,omitempty"`
	MaxOSDCombinations  *int32  `json:"max_osd_combinations,omitempty"`
	BlockSize           *int32  `json:"block_size,omitempty"`
	BitmapGranularity   *int32  `json:"bitmap_granularity,omitempty"`
	ImmediateCommit     *string `json:"immediate_commit,omitempty"`
	PGStripeSize        *int32  `json:"pg_stripe_size,omitempty"`
	RootNode            *string `json:"root_node,omitempty"`
	OSDTags             *string `json:"osd_tags,omitempty"`
	PrimaryAffinityTags *string `json:"primary_affinity_tags,omitempty"`
	ScrubInterval       *string `json:"scrub_interval,omitempty"`
	UsedForApp          *string `json:"used_for_app,omitempty"`
}

// VitastorClusterStats represents pool stats from Vitastor etcd
type VitastorClusterStats struct {
	PoolStats map[string]VitastorPoolStatEntry `json:"pool_stats"`
}

type VitastorPoolStatEntry struct {
	TotalRaw   float64 `json:"total_raw"`        // bytes
	UsedRaw    float64 `json:"used_raw"`         // bytes
	Efficiency float64 `json:"space_efficiency"` // ratio
}

const poolFinalizer = "vitastor.io/pool-finalizer"

//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorpools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorpools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorpools/finalizers,verbs=update
//+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
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

	var vitastorPool controlv2.VitastorPool
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: req.Name}, &vitastorPool); err != nil {
		log.Error(err, "unable to fetch VitastorPool, skipping")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	poolsPath := config.VitastorPrefix + "/config/pools"

	// Handle deletion
	if !vitastorPool.DeletionTimestamp.IsZero() {
		return r.handlePoolDeletion(ctx, &vitastorPool, cli, poolsPath)
	}

	// Add Finalizer if missing
	if !controllerutil.ContainsFinalizer(&vitastorPool, poolFinalizer) {
		controllerutil.AddFinalizer(&vitastorPool, poolFinalizer)
		if err := r.Update(ctx, &vitastorPool); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Check pool tree
	log.Info("Checking pools config")
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
	// 1. Проверяем, назначен ли ID в статусе CR
	poolID := vitastorPool.Status.ID

	// 2. Логика получения/генерации ID
	if poolID == int32(0) {
		// ID еще не назначен. Нужно сходить в Etcd и либо найти существующий, либо создать новый.
		assignedID, err := r.getOrCreatePoolID(ctx, cli, poolsPath, vitastorPool.Name, &vitastorPool)
		if err != nil {
			// Если ошибка оптимистичной блокировки (кто-то другой писал в это время),
			// просто вернем ошибку, контроллер перезапустится и попробует снова.
			return ctrl.Result{}, err
		}
		poolID = assignedID

		// Сохраняем ID в статус, чтобы больше не искать
		vitastorPool.Status.ID = poolID
		if err := r.Status().Update(ctx, &vitastorPool); err != nil {
			return ctrl.Result{}, err
		}

		// Requeue, чтобы на следующем проходе уже работать с зафиксированным ID
		return ctrl.Result{Requeue: true}, nil
	}

	// Дальше используем poolID (как string) для ключа мапы
	strPoolID := strconv.Itoa(int(poolID))
	pools[strPoolID] = r.getPoolConfig(&vitastorPool)
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
			sc, err := r.getStorageClassConfig(&vitastorPool, vitastorPool.Spec.VitastorFS)
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
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "Unable to get StorageClass")
		return ctrl.Result{}, err
	}

	// Update pool usage statistics (non-fatal)
	if err := r.updatePoolStatus(ctx, &vitastorPool, cli, config.VitastorPrefix); err != nil {
		log.Error(err, "Failed to update pool status stats (non-fatal)")
	}

	return ctrl.Result{}, nil
}

func (r *VitastorPoolReconciler) handlePoolDeletion(ctx context.Context, vitastorPool *controlv2.VitastorPool, cli *clientv3.Client, poolsPath string) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(vitastorPool, poolFinalizer) {
		return ctrl.Result{}, nil
	}

	// Safety check: block deletion if PersistentVolumes still use this pool's StorageClass
	pvList := &corev1.PersistentVolumeList{}
	if err := r.List(ctx, pvList); err != nil {
		log.Error(err, "Failed to list PersistentVolumes during pool deletion")
		return ctrl.Result{}, err
	}
	inUsePVs := []string{}
	for _, pv := range pvList.Items {
		if pv.Spec.StorageClassName == vitastorPool.Name {
			inUsePVs = append(inUsePVs, pv.Name)
		}
	}
	if len(inUsePVs) > 0 {
		log.Info("Pool deletion blocked: PersistentVolumes still exist", "pool", vitastorPool.Name, "pvs", inUsePVs)
		// Update status condition to reflect blocked deletion
		apimeta.SetStatusCondition(&vitastorPool.Status.Conditions, metav1.Condition{
			Type:    "Deleting",
			Status:  metav1.ConditionFalse,
			Reason:  "PVsExist",
			Message: fmt.Sprintf("Cannot delete pool: %d PersistentVolume(s) still reference this pool's StorageClass: %v", len(inUsePVs), inUsePVs),
		})
		if err := r.Status().Update(ctx, vitastorPool); err != nil {
			log.Error(err, "Failed to update pool status during blocked deletion")
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Remove pool from etcd
	poolID := vitastorPool.Status.ID
	if poolID != 0 {
		resp, err := cli.Get(ctx, poolsPath)
		if err != nil {
			log.Error(err, "Failed to get pools config during deletion")
			return ctrl.Result{}, err
		}
		if resp.Count > 0 {
			var pools map[string]VitastorPoolConfig
			if err := json.Unmarshal(resp.Kvs[0].Value, &pools); err != nil {
				log.Error(err, "Failed to parse pools config during deletion")
				return ctrl.Result{}, err
			}
			strPoolID := strconv.Itoa(int(poolID))
			if _, exists := pools[strPoolID]; exists {
				delete(pools, strPoolID)
				poolsBytes, err := json.Marshal(pools)
				if err != nil {
					return ctrl.Result{}, err
				}
				if _, err := cli.Put(ctx, poolsPath, string(poolsBytes)); err != nil {
					log.Error(err, "Failed to remove pool from etcd during deletion")
					return ctrl.Result{}, err
				}
				log.Info("Removed pool from etcd", "poolID", poolID, "pool", vitastorPool.Name)
			}
		}
	}

	// Remove finalizer so Kubernetes can complete the deletion (StorageClass is cleaned up via OwnerReference)
	controllerutil.RemoveFinalizer(vitastorPool, poolFinalizer)
	if err := r.Update(ctx, vitastorPool); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("Pool deleted successfully", "pool", vitastorPool.Name)
	return ctrl.Result{}, nil
}

func (r *VitastorPoolReconciler) updatePoolStatus(ctx context.Context, vitastorPool *controlv2.VitastorPool, cli *clientv3.Client, prefix string) error {
	statsResp, err := cli.Get(ctx, prefix+"/stats")
	if err != nil || statsResp.Count == 0 {
		return err
	}

	var clusterStats VitastorClusterStats
	if err := json.Unmarshal(statsResp.Kvs[0].Value, &clusterStats); err != nil {
		return nil // stats format unknown, skip
	}

	poolIDStr := strconv.Itoa(int(vitastorPool.Status.ID))
	entry, ok := clusterStats.PoolStats[poolIDStr]
	if !ok {
		return nil // not yet in stats
	}

	usedPct := float64(0)
	if entry.TotalRaw > 0 {
		usedPct = entry.UsedRaw / entry.TotalRaw * 100
	}

	vitastorPool.Status.Total = int64(entry.TotalRaw)
	vitastorPool.Status.Used = int64(entry.UsedRaw)
	vitastorPool.Status.Available = int64(entry.TotalRaw - entry.UsedRaw)
	vitastorPool.Status.UsedPercent = fmt.Sprintf("%.1f%%", usedPct)
	vitastorPool.Status.Efficiency = fmt.Sprintf("%.2f", entry.Efficiency)

	return r.Status().Update(ctx, vitastorPool)
}

func (r *VitastorPoolReconciler) getStorageClassConfig(pool *controlv2.VitastorPool, vitastorfs bool) (*storage.StorageClass, error) {

	storageClassParameters := map[string]string{
		"volumePrefix": "",
		"poolId":       pool.Spec.Name,
	}
	if vitastorfs {
		storageClassParameters["vitastorfs"] = "fs-meta"
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

func (r *VitastorPoolReconciler) getPoolConfig(vitastorPool *controlv2.VitastorPool) VitastorPoolConfig {
	poolSpec := VitastorPoolConfig{
		Name:                vitastorPool.Name,
		Scheme:              vitastorPool.Spec.Scheme,
		PGSize:              vitastorPool.Spec.PGSize,
		PGMinSize:           vitastorPool.Spec.PGMinSize,
		ParityChunks:        vitastorPool.Spec.ParityChunks,
		PGCount:             vitastorPool.Spec.PGCount,
		FailureDomain:       vitastorPool.Spec.FailureDomain,
		LevelPlacement:      vitastorPool.Spec.LevelPlacement,
		RawPlacement:        vitastorPool.Spec.RawPlacement,
		LocalReads:          vitastorPool.Spec.LocalReads,
		MaxOSDCombinations:  vitastorPool.Spec.MaxOSDCombinations,
		BlockSize:           vitastorPool.Spec.BlockSize,
		BitmapGranularity:   vitastorPool.Spec.BitmapGranularity,
		ImmediateCommit:     vitastorPool.Spec.ImmediateCommit,
		PGStripeSize:        vitastorPool.Spec.PGStripeSize,
		RootNode:            vitastorPool.Spec.RootNode,
		OSDTags:             vitastorPool.Spec.OSDTags,
		PrimaryAffinityTags: vitastorPool.Spec.PrimaryAffinityTags,
		ScrubInterval:       vitastorPool.Spec.ScrubInterval,
	}
	if vitastorPool.Spec.VitastorFS {
		appname := "fs:k8s-rwx"
		poolSpec.UsedForApp = &appname
	}

	return poolSpec
}

func (r *VitastorPoolReconciler) getOrCreatePoolID(ctx context.Context, cli *clientv3.Client, path string, poolName string, poolCR *controlv2.VitastorPool) (int32, error) {
	for i := 0; i < 3; i++ {
		resp, err := cli.Get(ctx, path)
		if err != nil {
			return 0, err
		}

		poolsMap := make(map[string]VitastorPoolConfig)
		var version int64 = 0

		if len(resp.Kvs) > 0 {
			version = resp.Kvs[0].Version
			if err := json.Unmarshal(resp.Kvs[0].Value, &poolsMap); err != nil {
				return 0, fmt.Errorf("failed to decode pools config: %w", err)
			}
		}

		for idStr, conf := range poolsMap {
			if conf.Name == poolName {
				id, _ := strconv.Atoi(idStr)
				return int32(id), nil
			}
		}

		var maxID int = 0
		for idStr := range poolsMap {
			id, err := strconv.Atoi(idStr)
			if err == nil && id > maxID {
				maxID = id
			}
		}
		newID := int32(maxID + 1)
		newIDStr := strconv.Itoa(int(newID))

		newConfig := r.getPoolConfig(poolCR)
		newConfig.Name = poolName

		poolsMap[newIDStr] = newConfig

		newData, _ := json.Marshal(poolsMap)

		txn := cli.Txn(ctx).If(
			clientv3.Compare(clientv3.Version(path), "=", version),
		).Then(
			clientv3.OpPut(path, string(newData)),
		)

		txnResp, err := txn.Commit()
		if err != nil {
			return 0, err
		}

		if txnResp.Succeeded {
			return newID, nil
		}
	}

	return 0, fmt.Errorf("failed to allocate pool ID after retries due to concurrent updates")
}

// SetupWithManager sets up the controller with the Manager.
func (r *VitastorPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlv2.VitastorPool{}).
		Owns(&storage.StorageClass{}).
		Complete(r)
}
