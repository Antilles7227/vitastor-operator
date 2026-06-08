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
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
)

// VitastorClusterReconciler reconciles a VitastorCluster object
type VitastorClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastornodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastornodes/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=daemonsets/status,verbs=get
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
//+kubebuilder:rbac:groups=v1,resources=configmaps,verbs=get;list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *VitastorClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var log = log.FromContext(ctx)

	// ===
	// Preparations, getting cluster CR, connect to Vitastor etcd, checking namespace for resources
	// ===
	var vitastorCluster controlv2.VitastorCluster
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: req.Name}, &vitastorCluster); err != nil {
		log.Error(err, "Unable to fetch VitastorCluster, skipping")
		return ctrl.Result{}, client.IgnoreNotFound(err)
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

	if vitastorCluster.Spec.VitastorClusterNamespace == "" {
		vitastorCluster.Spec.VitastorClusterNamespace = "vitastor-system"
	}
	defaultPlacementLevels := map[string]int32{
		"host": 100,
		"disk": 110,
		"osd":  120,
	}

	// ===
	// Cluster-wide settings
	// ===

	targetLevels := vitastorCluster.Spec.ClusterParameters.PlacementLevels
	if len(targetLevels) == 0 {
		targetLevels = defaultPlacementLevels
	}

	placementLevelBytes, _ := json.Marshal(targetLevels)
	nodePlacementPath := config.VitastorPrefix + "/config/placement_levels"

	// Idempotency check: Get before Put
	resp, err := cli.Get(ctx, nodePlacementPath)
	if err == nil {
		shouldUpdate := false
		if len(resp.Kvs) == 0 {
			shouldUpdate = true
		} else if string(resp.Kvs[0].Value) != string(placementLevelBytes) {
			shouldUpdate = true
		}

		if shouldUpdate {
			log.Info("Updating placement levels in Etcd")
			_, err := cli.Put(ctx, nodePlacementPath, string(placementLevelBytes))
			if err != nil {
				log.Error(err, "Failed to update placement levels")
			}
		}
	}

	// ===
	// Monitors
	// ===
	monitorDeployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: "vitastor-monitor", Namespace: vitastorCluster.Spec.VitastorClusterNamespace}, monitorDeployment); err != nil {
		if errors.IsNotFound(err) {
			// Deployment is not found - creating new one
			log.Info("Deployment is not found, creating new one")
			depl, err := r.getMonitorConfiguration(&vitastorCluster)
			if err != nil {
				log.Error(err, "Failed to create monitor deployment")
				return ctrl.Result{}, err
			}
			if err := controllerutil.SetControllerReference(&vitastorCluster, depl, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner for monitor deployment")
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, depl); err != nil {
				log.Error(err, "Failed to create new monitor Deployment")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "Failed to fetch monitor deployment")
		return ctrl.Result{}, err
	}
	// Image
	if monitorDeployment.Spec.Template.Spec.Containers[0].Image != vitastorCluster.Spec.Monitor.Image {
		log.Info("Monitor image mismatch, updating")
		monitorDeployment.Spec.Template.Spec.Containers[0].Image = vitastorCluster.Spec.Monitor.Image
		if err := r.Update(ctx, monitorDeployment); err != nil {
			log.Error(err, "Failed to update monitor deployment during image update")
			return ctrl.Result{}, err
		}
	}
	// Replicas
	if *monitorDeployment.Spec.Replicas != int32(vitastorCluster.Spec.Monitor.Replicas) {
		log.Info("Number of monitor replicas mismatch, updating")
		monitorDeployment.Spec.Replicas = &vitastorCluster.Spec.Monitor.Replicas
		if err := r.Update(ctx, monitorDeployment); err != nil {
			log.Error(err, "Failed to update monitor deployment during replica change")
			return ctrl.Result{}, err
		}
	}
	// Resources
	if !reflect.DeepEqual(monitorDeployment.Spec.Template.Spec.Containers[0].Resources, vitastorCluster.Spec.Monitor.Resources) {
		log.Info("resources of monitor deployment differs, updating")
		monitorDeployment.Spec.Template.Spec.Containers[0].Resources = vitastorCluster.Spec.Monitor.Resources
		if err := r.Update(ctx, monitorDeployment); err != nil {
			log.Error(err, "Failed to update monitor deployment during resource requirement change")
			return ctrl.Result{}, err
		}
	}

	// ===
	// Node Agent
	// ===
	agentDaemonSet := &appsv1.DaemonSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: "vitastor-agent", Namespace: vitastorCluster.Spec.VitastorClusterNamespace}, agentDaemonSet); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Daemonset is not found, creating new one")
			ds, err := r.getAgentConfiguration(&vitastorCluster)
			if err != nil {
				log.Error(err, "Failed to create agent daemonset")
				return ctrl.Result{}, err
			}
			if err := controllerutil.SetControllerReference(&vitastorCluster, ds, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner for agent daemonset")
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, ds); err != nil {
				log.Error(err, "Failed to create new agent Daemonset")
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Duration(5) * time.Minute}, nil
		}
		log.Error(err, "Failed to fetch agent daemonset")
		return ctrl.Result{}, err
	}
	// Image
	if agentDaemonSet.Spec.Template.Spec.Containers[0].Image != vitastorCluster.Spec.Agent.Image {
		log.Info("Agent image mismatch")
		agentDaemonSet.Spec.Template.Spec.Containers[0].Image = vitastorCluster.Spec.Agent.Image
		if err := r.Update(ctx, agentDaemonSet); err != nil {
			log.Error(err, "Failed to update agent daemonset image")
			return ctrl.Result{}, err
		}
	}
	// Resources
	if !reflect.DeepEqual(agentDaemonSet.Spec.Template.Spec.Containers[0].Resources, vitastorCluster.Spec.Agent.Resources) {
		log.Info("resources of agent daemonset differs, updating")
		agentDaemonSet.Spec.Template.Spec.Containers[0].Resources = vitastorCluster.Spec.Agent.Resources
		if err := r.Update(ctx, agentDaemonSet); err != nil {
			log.Error(err, "Failed to update agent daemonset during resource requirement change")
			return ctrl.Result{}, err
		}
	}
	// Node label
	if !reflect.DeepEqual(agentDaemonSet.Spec.Template.Spec.NodeSelector, map[string]string{vitastorCluster.Spec.VitastorNodeLabel: "true"}) {
		log.Info("NodeSelector label of agent daemonset differs, updating")
		agentDaemonSet.Spec.Template.Spec.NodeSelector = map[string]string{vitastorCluster.Spec.VitastorNodeLabel: "true"}
		if err := r.Update(ctx, agentDaemonSet); err != nil {
			log.Error(err, "Failed to update agent daemonset during NodeSelector label change")
			return ctrl.Result{}, err
		}
	}

	// ===
	// VitastorNode
	// ===
	nodeList := &corev1.NodeList{}
	getOpts := []client.ListOption{
		client.MatchingLabels{
			vitastorCluster.Spec.VitastorNodeLabel: "true",
		},
	}
	log.Info("Fetching nodes...")
	if err := r.List(ctx, nodeList, getOpts...); err != nil {
		log.Error(err, "unable to fetch Vitastor nodes")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	for _, node := range nodeList.Items {
		vitastorNode := &controlv2.VitastorNode{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: node.Name}, vitastorNode); err != nil {
			if errors.IsNotFound(err) {
				// VitastorNode CRD for that node is not found - creating new one
				log.Error(err, "VitastorNode CRD is not found, creating new one with default parameters", "NodeName", node.Name)
				newVitastorNode, err := r.getVitastorNodeConfiguration(node.Name, vitastorCluster.Name)
				if err != nil {
					log.Error(err, "Unable to get vitastorNode configuration")
					return ctrl.Result{}, err
				}
				log.Info("Created new VitastorNode CRD", "VitastorNode.name", newVitastorNode.Name)
				if err := controllerutil.SetControllerReference(&vitastorCluster, newVitastorNode, r.Scheme); err != nil {
					log.Error(err, "Failed to set owner for vitastorNode CRD")
					return ctrl.Result{}, err
				}
				if err := r.Create(ctx, newVitastorNode); err != nil {
					log.Error(err, "Unable to create VitastorNode CRD")
					return ctrl.Result{}, err
				}
			}
		}
	}

	if err := r.reconcileRollingUpdates(ctx, &vitastorCluster); err != nil {
		log.Error(err, "Failed to coordinate rolling updates")
	}

	return ctrl.Result{}, nil
}

func (r *VitastorClusterReconciler) reconcileRollingUpdates(ctx context.Context, cluster *controlv2.VitastorCluster) error {
	// 1. Получаем список всех OSD этого кластера
	osdList := &controlv2.VitastorOSDList{}
	if err := r.List(ctx, osdList, client.MatchingLabels{"vitastor.io/cluster": cluster.Name}); err != nil {
		return err
	}

	// 2. Проверяем текущего активного кандидата
	activeOSDName := cluster.Status.ActiveOSD
	if activeOSDName != "" {
		// Проверяем статус этого OSD
		var activeOSD controlv2.VitastorOSD
		found := false
		for _, o := range osdList.Items {
			if o.Name == activeOSDName {
				activeOSD = o
				found = true
				break
			}
		}

		if !found {
			// OSD удален? Сбрасываем лок
			cluster.Status.ActiveOSD = ""
			return r.Status().Update(ctx, cluster)
		}

		if activeOSD.Status.State == OSDStateRunning {
			// TODO: Check rebalance status with cli
			log.FromContext(ctx).Info("OSD updated successfully, releasing lock", "osd", activeOSDName)
			cluster.Status.ActiveOSD = ""
			return r.Status().Update(ctx, cluster)
		}

		return nil
	}

	for _, osd := range osdList.Items {
		if osd.Status.State == OSDStateUpdateRequired {
			log.FromContext(ctx).Info("Locking cluster for OSD update", "osd", osd.Name)
			cluster.Status.ActiveOSD = osd.Name
			return r.Status().Update(ctx, cluster)
		}
	}

	return nil
}

func (r *VitastorClusterReconciler) getVitastorNodeConfiguration(nodeName string, clusterName string) (*controlv2.VitastorNode, error) {
	vitastorNode := &controlv2.VitastorNode{
		ObjectMeta: ctrl.ObjectMeta{
			Name: nodeName,
			Labels: map[string]string{
				"control.vitastor.io/cluster": clusterName,
			},
		},
		Spec: controlv2.VitastorNodeSpec{
			NoOut:  false,
			Weight: "1.0",
		},
	}
	return vitastorNode, nil
}

func (r *VitastorClusterReconciler) getMonitorConfiguration(cluster *controlv2.VitastorCluster) (*appsv1.Deployment, error) {
	monitorReplicas := int32(cluster.Spec.Monitor.Replicas)
	labels := map[string]string{
		"control.vitastor.io/app":     "vitastor-monitor",
		"control.vitastor.io/cluster": cluster.Name,
	}

	depl := appsv1.Deployment{
		ObjectMeta: ctrl.ObjectMeta{
			Namespace: cluster.Spec.VitastorClusterNamespace,
			Name:      "vitastor-monitor",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &monitorReplicas,
			Selector: &v1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:      "vitastor-monitor",
							Image:     cluster.Spec.Monitor.Image,
							Resources: cluster.Spec.Monitor.Resources,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "vitastor-config",
									MountPath: "/etc/vitastor",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
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
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{IntVal: 1},
					MaxSurge:       &intstr.IntOrString{IntVal: 1},
				},
			},
		},
	}
	return &depl, nil
}

func (r *VitastorClusterReconciler) getAgentConfiguration(cluster *controlv2.VitastorCluster) (*appsv1.DaemonSet, error) {
	privilegedContainer := true
	dsLabels := map[string]string{
		"control.vitastor.io/app":     "vitastor-agent",
		"control.vitastor.io/cluster": cluster.Name,
	}
	nodeLabels := map[string]string{cluster.Spec.VitastorNodeLabel: "true"}

	ds := appsv1.DaemonSet{
		ObjectMeta: ctrl.ObjectMeta{
			Namespace: cluster.Spec.VitastorClusterNamespace,
			Name:      "vitastor-agent",
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &v1.LabelSelector{
				MatchLabels: dsLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Labels: dsLabels,
				},
				Spec: corev1.PodSpec{
					NodeSelector: nodeLabels,
					HostNetwork:  true,
					Containers: []corev1.Container{
						{
							Name:  "vitastor-agent",
							Image: cluster.Spec.Agent.Image,
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privilegedContainer,
							},
							Resources: cluster.Spec.Agent.Resources,
							Ports:     []corev1.ContainerPort{{ContainerPort: 8000}},
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
						},
					},
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
			},
		},
	}
	return &ds, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VitastorClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&controlv2.VitastorCluster{}).
		Owns(&controlv2.VitastorNode{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
