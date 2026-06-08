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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
)

const (
	OSDStateRunning        = "Running"
	OSDStateUpdateRequired = "UpdateRequired"
	OSDStateUpdating       = "Updating"
)

// VitastorOSDReconciler reconciles a VitastorOSD object
type VitastorOSDReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *VitastorOSDReconciler) getConfiguration(osd *controlv2.VitastorOSD, cluster *controlv2.VitastorCluster) (*corev1.Pod, error) {
	imageName := cluster.Spec.OSD.Image

	containerPort := int32(5666) //container port, for now hardcoded
	privilegedContainer := true

	labels := map[string]string{
		"control.vitastor.io/cluster": cluster.Name,
		"control.vitastor.io/node":    osd.Labels["control.vitastor.io/node"],
		"control.vitastor.io/disk":    osd.Labels["control.vitastor.io/disk"],
	}

	pod := corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Labels:      labels,
			Annotations: map[string]string{},
			Name:        "vitastor-osd-" + strconv.Itoa(int(osd.Spec.Id)),
		},
		Spec: corev1.PodSpec{
			NodeName: osd.Labels["control.vitastor.io/node"],
			Containers: []corev1.Container{
				{
					Name:      "vitastor-osd",
					Image:     imageName,
					Command:   []string{"vitastor-disk"},
					Args:      []string{"exec-osd", osd.Spec.Path},
					Resources: cluster.Spec.OSD.Resources,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "vitastor-config",
							MountPath: "/etc/vitastor",
						},
						{
							Name:      "host-dev",
							MountPath: "/dev",
						},
						{
							Name:      "host-sys",
							MountPath: "/sys",
						},
						{
							Name:      "host-lib-modules",
							MountPath: "/lib/modules",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: &privilegedContainer,
					},
					Ports: []corev1.ContainerPort{{ContainerPort: containerPort}},
				},
			},
			PriorityClassName: "system-cluster-critical",
			Volumes: []corev1.Volume{
				{
					Name: "host-dev",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/dev",
						},
					},
				},
				{
					Name: "host-sys",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/sys",
						},
					},
				},
				{
					Name: "host-lib-modules",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/lib/modules",
						},
					},
				},
				{
					Name: "vitastor-config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "vitastor-config"},
						},
					},
				},
			},
		},
	}
	return &pod, nil
}

//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorosds,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorosds/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorosds/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets/status,verbs=get
//+kubebuilder:rbac:groups=v1,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=v1,resources=configmaps,verbs=get;list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *VitastorOSDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var log = log.FromContext(ctx)

	var vitastorOSD controlv2.VitastorOSD
	err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: req.Name}, &vitastorOSD)
	if err != nil {
		log.Error(err, "unable to fetch VitastorOSD, seems it's destroyed")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var ownerCluster controlv2.VitastorCluster
	if err := r.Client.Get(ctx, types.NamespacedName{Name: vitastorOSD.Labels["control.vitastor.io/cluster"], Namespace: corev1.NamespaceAll}, &ownerCluster); err != nil {
		log.Error(err, "unable to fetch owner VitastorCluster")
		return ctrl.Result{}, err
	}

	if vitastorOSD.Status.State == OSDStateUpdateRequired {
		if ownerCluster.Status.ActiveOSD == vitastorOSD.Name {
			vitastorOSD.Status.State = OSDStateUpdating
			if err := r.Status().Update(ctx, &vitastorOSD); err != nil {
				log.Error(err, "failed to update OSD status", "osd.Name", vitastorOSD.Name)
				return ctrl.Result{}, err
			}
			if err := r.syncOSDConfigToEtcd(ctx, &vitastorOSD); err != nil {
				log.Error(err, "Failed to sync OSD config to etcd during update", "osd", vitastorOSD.Spec.Id)
			}
		}
	}

	// Check if Pod already exists, if not create a new one
	foundOsdPod := &corev1.Pod{}
	err = r.Get(ctx, types.NamespacedName{Name: vitastorOSD.Name, Namespace: ownerCluster.Spec.VitastorClusterNamespace}, foundOsdPod)
	if err != nil {
		if errors.IsNotFound(err) {
			// Pod is not found - creating new one
			log.Info("Pod is not found, creating new one")
			pod, err := r.getConfiguration(&vitastorOSD, &ownerCluster)
			if err != nil {
				log.Error(err, "Failed to create new Pod object", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
				return ctrl.Result{}, err
			}
			hex, err := contentHash(pod)
			if err != nil {
				log.Error(err, "Failed to compute Pod content hash", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
				return ctrl.Result{}, err
			}
			pod.Annotations["control.vitastor.io/content-hash"] = hex
			if err := controllerutil.SetControllerReference(&vitastorOSD, pod, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner for OSD pod")
				return ctrl.Result{}, err
			}
			err = r.Create(ctx, pod)
			if err != nil {
				log.Error(err, "Failed to create new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "Failed to fetch OSD pod")
		return ctrl.Result{}, err
	}

	//Check OSD content hash
	pod, err := r.getConfiguration(&vitastorOSD, &ownerCluster)
	if err != nil {
		log.Error(err, "Failed to create new Pod object", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		return ctrl.Result{}, err
	}
	contentHash, err := contentHash(pod)
	if err != nil {
		log.Error(err, "Failed to compute Pod content hash", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		return ctrl.Result{}, err
	}
	if foundOsdPod.Annotations["control.vitastor.io/content-hash"] != contentHash {
		log.Info("OSD image mismatch, updating state", "osd", vitastorOSD.Spec.Id)
		vitastorOSD.Status.State = OSDStateUpdateRequired
		if err := r.Status().Update(ctx, &vitastorOSD); err != nil {
			log.Error(err, "failed to update OSD status", "osd.Name", vitastorOSD.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Duration(10) * time.Second}, nil
	} else {
		log.Info("OSD started", "osd", vitastorOSD.Spec.Id)
		vitastorOSD.Status.State = OSDStateRunning
		if err := r.Status().Update(ctx, &vitastorOSD); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Sync OSD maintenance config (noout, weight, tags) to Vitastor etcd
	if err := r.syncOSDConfigToEtcd(ctx, &vitastorOSD); err != nil {
		log.Error(err, "Failed to sync OSD config to etcd", "osd", vitastorOSD.Spec.Id)
		// Non-fatal: log and continue, will retry on next reconcile
	}

	return ctrl.Result{}, nil
}

func contentHash(pod *corev1.Pod) (string, error) {
	type HashableSpec struct {
		Image     string
		Resources corev1.ResourceRequirements
		Command   []string
		Args      []string
		Env       []corev1.EnvVar
	}
	c := pod.Spec.Containers[0]

	hSpec := HashableSpec{
		Image:     c.Image,
		Resources: c.Resources,
		Command:   c.Command,
		Args:      c.Args,
		Env:       c.Env,
	}

	data, err := json.Marshal(hSpec)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// OSDEtcdConfig represents the OSD-specific configuration stored in Vitastor etcd.
type OSDEtcdConfig struct {
	Noout    bool     `json:"noout,omitempty"`
	Reweight *float64 `json:"reweight,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

func (r *VitastorOSDReconciler) syncOSDConfigToEtcd(ctx context.Context, osd *controlv2.VitastorOSD) error {
	config, err := loadConfiguration(ctx, "/etc/vitastor/vitastor.conf")
	if err != nil {
		return fmt.Errorf("loading vitastor.conf: %w", err)
	}
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   config.VitastorEtcdUrls,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("connecting to etcd: %w", err)
	}
	defer cli.Close()

	osdKey := fmt.Sprintf("%s/config/osd/%d", config.VitastorPrefix, osd.Spec.Id)

	// Build desired config
	desired := OSDEtcdConfig{
		Noout: osd.Spec.NoOut,
	}
	// Parse weight as float64
	if osd.Spec.Weight != "" {
		var w float64
		if _, err := fmt.Sscanf(osd.Spec.Weight, "%f", &w); err == nil {
			desired.Reweight = &w
		}
	}
	// Store tags as JSON array
	if len(osd.Spec.Tags) > 0 {
		desired.Tags = osd.Spec.Tags
	}

	desiredBytes, err := json.Marshal(desired)
	if err != nil {
		return err
	}

	// Read current value (idempotency check)
	resp, err := cli.Get(ctx, osdKey)
	if err != nil {
		return err
	}
	if len(resp.Kvs) > 0 && string(resp.Kvs[0].Value) == string(desiredBytes) {
		return nil // already in sync
	}

	_, err = cli.Put(ctx, osdKey, string(desiredBytes))
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *VitastorOSDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlv2.VitastorOSD{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
