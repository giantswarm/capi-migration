/*
Copyright 2021 Giant Swarm.

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

	"github.com/giantswarm/microerror"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capiv1alpha3 "sigs.k8s.io/cluster-api/api/v1alpha3"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cluster.x-k8s.io.giantswarm.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io.giantswarm.io,resources=clusters/status,verbs=get;update;patch

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("cluster", req.NamespacedName)

	cluster := &capiv1alpha3.Cluster{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if err != nil {
		return ctrl.Result{}, microerror.Mask(err)
	}

	// Based on https://github.com/kubernetes-sigs/cluster-api/blob/master/controllers/machine_controller.go.
	if cluster.DeletionTimestamp.IsZero() {
		res, err := r.reconcileDelete(ctx, log, cluster)
		if err != nil {
			return ctrl.Result{}, microerror.Mask(err)
		}

		return res, nil
	}

	res, err := r.reconcile(ctx, log, cluster)
	if err != nil {
		return ctrl.Result{}, microerror.Mask(err)
	}

	return res, nil
}

func (r *ClusterReconciler) reconcile(ctx context.Context, log logr.Logger, cluster *capiv1alpha3.Cluster) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *ClusterReconciler) reconcileDelete(ctx context.Context, log logr.Logger, cluster *capiv1alpha3.Cluster) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capiv1alpha3.Cluster{}).
		Complete(r)
}
