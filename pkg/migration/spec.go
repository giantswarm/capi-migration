// Package migration provides implementation for CR migration from Giant Swarm
// flavor to upstream compatible CAPI.
package migration

type MigratorFactory interface {
	// Construct new Migrator for given cluster.
	NewMigrator(clusterID string) (Migrator, error)
}

type Migrator interface {
	// Cleanup performs cleanup operations after migration has been completed.
	Cleanup() error

	// IsMigrated performs check to see if given cluster has been already
	// migrated.
	IsMigrated() (bool, error)

	// IsMigrating performs check to see if given cluster has migration
	// triggered already.
	IsMigrating() (bool, error)

	// Prepare executes preparatory migration actions such as transforming
	// existing CRs into upstream compatible format and creating missing CRs.
	Prepare() error

	// TriggerMigration performs final execution which shifts reconciliation to
	// upstream controllers.
	TriggerMigration() error
}
