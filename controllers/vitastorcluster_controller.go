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
	"os"
	"time"

	"go.etcd.io/etcd/client/v3"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	controlv1 "gitlab.com/Antilles7227/vitastor-operator/api/v1"
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

	var vitastorCluster controlv1.VitastorCluster
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: req.Name}, &vitastorCluster); err != nil {
		log.Error(err, "Unable to fetch VitastorCluster, skipping")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	monitorReplicas := int32(vitastorCluster.Spec.MonitorReplicaNum)

	// Check monitor deployment
	monitorDeployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: "vitastor-monitor", Namespace: namespace}, monitorDeployment); err != nil {
		if errors.IsNotFound(err) {
			// Deployment is not found - creating new one
			log.Info("Deployment is not found, creating new one")
			depl, err := r.getMonitorConfiguration(&vitastorCluster)
			if err != nil {
				log.Error(err, "Failed to create monitor deployment")
				return ctrl.Result{}, err
			}
			if err := controllerutil.SetControllerReference(&vitastorCluster, depl, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner deployment")
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, depl); err != nil {
				log.Error(err, "Failed to create new Deployment")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "Failed to fetch monitor deployment")
		return ctrl.Result{}, err
	}

	// Check monitor image
	if monitorDeployment.Spec.Template.Spec.Containers[0].Image != vitastorCluster.Spec.MonitorImage {
		log.Info("Monitor image mismatch, updating")
		monitorDeployment.Spec.Template.Spec.Containers[0].Image = vitastorCluster.Spec.MonitorImage
		if err := r.Update(ctx, monitorDeployment); err != nil {
			log.Error(err, "Failed to update monitor deployment")
			return ctrl.Result{}, err
		}
	}
	// Check monitor replicas
	if *monitorDeployment.Spec.Replicas != int32(vitastorCluster.Spec.MonitorReplicaNum) {
		log.Info("Number of monitor replicas mismatch, updating")
		monitorDeployment.Spec.Replicas = &monitorReplicas
		if err := r.Update(ctx, monitorDeployment); err != nil {
			log.Error(err, "Failed to update monitor deployment")
			return ctrl.Result{}, err
		}
	}

	// Check agent daemonset
	agentDaemonSet := &appsv1.DaemonSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: "vitastor-agent", Namespace: namespace}, agentDaemonSet); err != nil {
		if errors.IsNotFound(err) {
			// Daemonset is not found - creating new one
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
				log.Error(err, "Failed to create new Daemonset")
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Duration(5) * time.Minute}, nil
		}
		log.Error(err, "Failed to fetch agent daemonset")
		return ctrl.Result{}, err
	}

	// Check agent image
	if agentDaemonSet.Spec.Template.Spec.Containers[0].Image != vitastorCluster.Spec.AgentImage {
		log.Info("Agent image mismatch")
		agentDaemonSet.Spec.Template.Spec.Containers[0].Image = vitastorCluster.Spec.AgentImage
		if err := r.Update(ctx, agentDaemonSet); err != nil {
			log.Error(err, "Failed to update agent daemonset")
			return ctrl.Result{}, err
		}
	}

	agentList := &corev1.PodList{}
	getOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{"app": "vitastor-agent"},
	}
	log.Info("Fetching agents...")
	if err := r.List(ctx, agentList, getOpts...); err != nil {
		log.Error(err, "unable to fetch Vitastor agents")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	for _, agent := range agentList.Items {
		vitastorNode := &controlv1.VitastorNode{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: agent.Spec.NodeName}, vitastorNode); err != nil {
			if errors.IsNotFound(err) {
				// VitastorNode CRD for that node is not found - creating new one
				log.Error(err, "VitastorNode CRD is not found, creating new one", "NodeName", agent.Spec.NodeName)
				newVitastorNode, err := r.getVitastorNodeConfiguration(agent.Spec.NodeName)
				if err != nil {
					log.Error(err, "Unable to get vitastorNode configuration")
					return ctrl.Result{}, err
				}
				log.Info("Created new VitastorNode CRD", "VitastorNode.name", newVitastorNode.Name, "VitastorNode.Spec.NodeName", newVitastorNode.Spec.NodeName)
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

	// Checking OSD images
	oldOSD := &controlv1.VitastorOSDList{}
	newOSD := &controlv1.VitastorOSDList{}
	oldOSD, newOSD, err = r.getOSDList(&vitastorCluster, namespace, ctx)
	if err != nil {
		log.Error(err, "Error during OSD Statefulsets fetching")
		return ctrl.Result{}, err
	}
	for _, osd := range(newOSD.Items) {
		osdSts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, types.NamespacedName{Name: osd.Name, Namespace: namespace}, osdSts); err != nil {
			log.Error(err, "New VitastorOSD StatefulSet is not found, skipping")
			return ctrl.Result{}, err
		}
		if osdSts.Status.Replicas != osdSts.Status.ReadyReplicas {
			log.Info("There are offline OSD with new version, delaying upgrade for a 10 seconds for next check until all new OSD is up and running")
			return ctrl.Result{RequeueAfter: time.Duration(10) * time.Second}, nil
		}
	}
	if len(oldOSD.Items) > 0 {
		log.Info("Updating OSD for new version")
		oldOSD.Items[0].Spec.OSDImage = vitastorCluster.Spec.OSDImage
		if err := r.Update(ctx, &oldOSD.Items[0]); err != nil {
			log.Error(err, "Failed to update OSD CRD")
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Duration(10) * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *VitastorClusterReconciler) getOSDList(cluster *controlv1.VitastorCluster, namespace string, ctx context.Context) (*controlv1.VitastorOSDList, *controlv1.VitastorOSDList, error) {
	var log = log.FromContext(ctx)
	oldOSD := &controlv1.VitastorOSDList{}
	newOSD := &controlv1.VitastorOSDList{}
	
	getOptsNew := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingFields{"spec.osdImage": cluster.Spec.OSDImage},
	}
	getOptsOld := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingFieldsSelector{

		},
	}
	log.Info("Fetching agents...")
	if err := r.List(ctx, newOSD, getOptsNew...); err != nil {
		log.Error(err, "unable to fetch Vitastor agents")
		return nil, nil, client.IgnoreNotFound(err)
	}

	return &controlv1.VitastorOSDList{}, &controlv1.VitastorOSDList{}, nil
}

func (r *VitastorClusterReconciler) getVitastorNodeConfiguration(nodeName string) (*controlv1.VitastorNode, error) {
	vitastorNode := &controlv1.VitastorNode{
		ObjectMeta: ctrl.ObjectMeta{
			Name: nodeName,
		},
		Spec: controlv1.VitastorNodeSpec{
			NodeName: nodeName,
		},
	}
	return vitastorNode, nil
}

func (r *VitastorClusterReconciler) getMonitorConfiguration(cluster *controlv1.VitastorCluster) (*appsv1.Deployment, error) {
	namespace, isEmpty := os.LookupEnv("NAMESPACE")
	if !isEmpty {
		namespace = "vitastor-system"
	}
	monitorReplicas := int32(cluster.Spec.MonitorReplicaNum)
	labels := map[string]string{"app": "vitastor-monitor"}

	depl := appsv1.Deployment{
		ObjectMeta: ctrl.ObjectMeta{
			Namespace: namespace,
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
							Name:  "vitastor-monitor",
							Image: cluster.Spec.MonitorImage,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1000m"),
									corev1.ResourceMemory: resource.MustParse("1024Mi"),
								},
							},
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
			},
		},
	}
	return &depl, nil
}

func (r *VitastorClusterReconciler) getAgentConfiguration(cluster *controlv1.VitastorCluster) (*appsv1.DaemonSet, error) {
	namespace, isEmpty := os.LookupEnv("NAMESPACE")
	if !isEmpty {
		namespace = "vitastor-system"
	}
	privilegedContainer := true
	dsLabels := map[string]string{"app": "vitastor-agent"}
	nodeLabels := map[string]string{cluster.Spec.VitastorNodeLabel: "true"}

	ds := appsv1.DaemonSet{
		ObjectMeta: ctrl.ObjectMeta{
			Namespace: namespace,
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
					Containers: []corev1.Container{
						{
							Name:  "vitastor-agent",
							Image: cluster.Spec.AgentImage,
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privilegedContainer,
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1000m"),
									corev1.ResourceMemory: resource.MustParse("1024Mi"),
								},
							},
							Ports: []corev1.ContainerPort{{ContainerPort: 8000}},
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
		For(&controlv1.VitastorCluster{}).
		Owns(&controlv1.VitastorNode{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
