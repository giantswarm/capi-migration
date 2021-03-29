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
	"strconv"
	"sync/atomic"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/giantswarm/micrologger/loggermeta"
	"github.com/giantswarm/tenantcluster/v3/pkg/tenantcluster"
	vaultapi "github.com/hashicorp/vault/api"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	capiv1alpha3 "sigs.k8s.io/cluster-api/api/v1alpha3"

	"github.com/giantswarm/capi-migration/pkg/meta"
	"github.com/giantswarm/capi-migration/pkg/migration"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Log             micrologger.Logger
	MigratorFactory migration.MigratorFactory
	TenantCluster   tenantcluster.TenantCluster
	VaultClient     *vaultapi.Client
	Scheme          *runtime.Scheme

	loopSeq int64
}

// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io.giantswarm.io,resources=clusters/status,verbs=get;update;patch

func (r *ClusterReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	// TODO get context as parameter as soon as we bump sigs.k8s.io/controller-runtime to 0.7+.
	ctx := context.Background()

	meta := loggermeta.New()
	meta.KeyVals = map[string]string{
		"controller": "cluster",
		"object":     req.NamespacedName.String(),
		"loop":       strconv.FormatInt(atomic.AddInt64(&r.loopSeq, 1), 10),
	}
	ctx = loggermeta.NewContext(ctx, meta)

	cluster := &capiv1alpha3.Cluster{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if err != nil {
		return ctrl.Result{}, microerror.Mask(err)
	}

	// Based on https://github.com/kubernetes-sigs/cluster-api/blob/master/controllers/machine_controller.go.
	if !cluster.DeletionTimestamp.IsZero() {
		res, err := r.reconcileDelete(ctx, cluster)
		if err != nil {
			requeueAfter := 30 * time.Second
			r.Log.Errorf(ctx, err, "failed to reconcile, requeuing after %#q", requeueAfter)
			return ctrl.Result{RequeueAfter: requeueAfter}, nil
		}

		return res, nil
	}

	res, err := r.reconcile(ctx, cluster)
	if err != nil {
		requeueAfter := 30 * time.Second
		r.Log.Errorf(ctx, err, "failed to reconcile, requeuing after %#q", requeueAfter)
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	return res, nil
}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capiv1alpha3.Cluster{}).
		WithEventFilter(predicate.NewPredicateFuncs(meta.Label.Version.Predicate)).
		Complete(r)
}

func (r *ClusterReconciler) reconcile(ctx context.Context, cluster *capiv1alpha3.Cluster) (ctrl.Result, error) {
	r.Log.Debugf(ctx, "calling reconcile")

	migrator, err := r.MigratorFactory.NewMigrator(cluster)
	if err != nil {
		return ctrl.Result{}, microerror.Mask(err)
	}

	alreadyMigrated, err := migrator.IsMigrated(ctx)
	if err != nil {
		return ctrl.Result{}, microerror.Mask(err)
	}

	if alreadyMigrated {
		r.Log.Debugf(ctx, "cluster is already migrated")
		// Migration performed. Cleanup.
		err = migrator.Cleanup(ctx)
		if err != nil {
			return ctrl.Result{}, microerror.Mask(err)
		}

		return ctrl.Result{}, nil
	}

	migrating, err := migrator.IsMigrating(ctx)
	if err != nil {
		return ctrl.Result{}, microerror.Mask(err)
	}

	if migrating {
		// Migration has been triggered but it's not complete yet.
		r.Log.Debugf(ctx, "cluster migration is in progress")
		return ctrl.Result{}, nil
	}

	r.Log.Debugf(ctx, "preparing cluster migration")
	err = migrator.Prepare(ctx)
	if err != nil {
		return ctrl.Result{}, microerror.Mask(err)
	}

	r.Log.Debugf(ctx, "triggering cluster migration")
	err = migrator.TriggerMigration(ctx)
	if err != nil {
		return ctrl.Result{}, microerror.Mask(err)
	}

	return ctrl.Result{}, nil
}

func (r *ClusterReconciler) reconcileDelete(ctx context.Context, cluster *capiv1alpha3.Cluster) (ctrl.Result, error) {
	r.Log.Debugf(ctx, "calling reconcileDelete")
	return ctrl.Result{}, nil
}
