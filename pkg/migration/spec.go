// Package migration provides implementation for CR migration from Giant Swarm
// flavor to upstream compatible CAPI.
package migration

import (
	"context"

	"sigs.k8s.io/cluster-api/api/v1alpha3"
)

type MigratorFactory interface {
	// Construct new Migrator for given cluster.
	NewMigrator(cluster *v1alpha3.Cluster) (Migrator, error)
}

type Migrator interface {
	// Cleanup performs cleanup operations after migration has been completed.
	Cleanup(ctx context.Context) error

	// IsMigrated performs check to see if given cluster has been already
	// migrated.
	IsMigrated(ctx context.Context) (bool, error)

	// IsMigrating performs check to see if given cluster has migration
	// triggered already.
	IsMigrating(ctx context.Context) (bool, error)

	// Prepare executes preparatory migration actions such as transforming
	// existing CRs into upstream compatible format and creating missing CRs.
	Prepare(ctx context.Context) error

	// TriggerMigration performs final execution which shifts reconciliation to
	// upstream controllers.
	TriggerMigration(ctx context.Context) error
}
