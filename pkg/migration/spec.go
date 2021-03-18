// Package migration provides implementation for CR migration from Giant Swarm
// flavor to upstream compatible CAPI.
package migration

type Migrator interface {
	Migrate() error
}
