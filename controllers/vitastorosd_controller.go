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
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	controlv1 "gitlab.com/Antilles7227/vitastor-operator/api/v1"
)

// VitastorOSDReconciler reconciles a VitastorOSD object
type VitastorOSDReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *VitastorOSDReconciler) getConfiguration(osd *controlv1.VitastorOSD) (*appsv1.StatefulSet, error) {
	namespace, isEmpty := os.LookupEnv("NAMESPACE")
	if !isEmpty {
		namespace = "vitastor-system"
	}
	imageName := osd.Spec.OSDImage
	containerPortRaw, isEmpty := os.LookupEnv("CONTAINER_PORT")
	if !isEmpty {
		containerPortRaw = "5666"
	}
	cp, err := strconv.Atoi(containerPortRaw)
	if err != nil {
		return nil, err
	}
	containerPort := int32(cp)
	privilegedContainer := true
	nodeName := osd.Spec.NodeName
	osdPath := osd.Spec.OSDPath
	replicas := int32(1)

	labels := map[string]string{"osdName": osd.Name, "node": nodeName}

	depl := appsv1.StatefulSet{
		ObjectMeta: ctrl.ObjectMeta{
			Namespace: namespace,
			Name:      osd.Name,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &v1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName,
					Containers: []corev1.Container{
						{
							Name:    "vitastor-osd",
							Image:   imageName,
							Command: []string{"vitastor-disk"},
							Args:    []string{"exec-osd", osdPath},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("1000m"),
								},
							},
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
			},
		},
	}
	return &depl, nil
}

//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorosds,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorosds/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=control.vitastor.io,resources=vitastorosds/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets/status,verbs=get
//+kubebuilder:rbac:groups=v1,resources=configmaps,verbs=get;list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *VitastorOSDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var log = log.FromContext(ctx)
	namespace, isEmpty := os.LookupEnv("NAMESPACE")
	if !isEmpty {
		namespace = "vitastor-system"
	}

	var vitastorOSD controlv1.VitastorOSD
	err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: req.Name}, &vitastorOSD)
	if err != nil {
		log.Error(err, "unable to fetch VitastorOSD, seems it's destroyed")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if deployment already exists, if not create a new deployment
	foundSts := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: vitastorOSD.Name, Namespace: namespace}, foundSts)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment is not found - creating new one
			log.Info("Deployment is not found, creating new one")
			sts, err := r.getConfiguration(&vitastorOSD)
			if err := controllerutil.SetControllerReference(&vitastorOSD, sts, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner for deployment")
				return ctrl.Result{}, err
			}
			if err != nil {
				log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", sts.Namespace, "Deployment.Name", sts.Name)
				return ctrl.Result{}, err
			}
			err = r.Create(ctx, sts)
			if err != nil {
				log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", sts.Namespace, "Deployment.Name", sts.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "Failed to fetch osd deployment")
		return ctrl.Result{}, err
	}

	//Check OSD image
	if foundSts.Spec.Template.Spec.Containers[0].Image != vitastorOSD.Spec.OSDImage {
		log.Info("OSD image mismatch")
		foundSts.Spec.Template.Spec.Containers[0].Image = vitastorOSD.Spec.OSDImage
		if err := r.Update(ctx, foundSts); err != nil {
			log.Error(err, "Failed to update OSD statefulset")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VitastorOSDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlv1.VitastorOSD{}).
		Owns(&appsv1.StatefulSet{}).
		Complete(r)
}
