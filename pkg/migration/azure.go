package migration

import (
	"context"
	"fmt"

	"github.com/giantswarm/microerror"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

type AzureMigrationConfig struct {
	// Migration configuration + dependencies such as k8s client.
	ctrlClient ctrl.Client
}

type azureMigratorFactory struct {
	config AzureMigrationConfig
}

type azureMigrator struct {
	clusterID string

	ctrlClient ctrl.Client

	crs map[string]runtime.Object

	// Migration configuration, dependencies + intermediate cache for involved
	// CRs.
}

func NewAzureMigratorFactory(cfg AzureMigrationConfig) (MigratorFactory, error) {
	return &azureMigratorFactory{
		config: cfg,
	}, nil
}

func (f *azureMigratorFactory) NewMigrator(clusterID string) (Migrator, error) {
	return &azureMigrator{
		clusterID: clusterID,
		// rest of the config from f.config...
	}, nil
}

func (m *azureMigrator) IsMigrated(ctx context.Context) (bool, error) {
	return false, nil
}

func (m *azureMigrator) IsMigrating(ctx context.Context) (bool, error) {
	return false, nil
}

func (m *azureMigrator) Prepare(ctx context.Context) error {
	var err error

	err = m.readCRs(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.prepareMissingCRs(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.updateCRs(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) TriggerMigration(ctx context.Context) error {
	err := m.triggerMigration(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) Cleanup(ctx context.Context) error {
	migrated, err := m.IsMigrated(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	if !migrated {
		return fmt.Errorf("cluster has not migrated yet")
	}

	return nil
}

// readCRs reads existing CRs involved in migration. For Azure this contains
// roughly following CRs:
// - AzureConfig
// - Cluster
// - AzureCluster
// - MachinePools
// - AzureMachinePools
//
func (m *azureMigrator) readCRs(ctx context.Context) error {
	var err error

	err = m.readEncryptionSecret(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.readAzureConfig(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.readCluster(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.readAzureCluster(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.readMachinePools(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.readAzureMachinePools(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// prepareMissingCRs constructs missing CRs that are needed for CAPI+CAPZ
// reconciliation to work. This include e.g. KubeAdmControlPlane and
// AzureMachineTemplate for new master nodes.
func (m *azureMigrator) prepareMissingCRs(ctx context.Context) error {
	var err error

	err = m.createEncryptionConfigSecret(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createProxyConfigSecret(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createKubeadmControlPlane(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createMasterAzureMachineTemplate(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createWorkersKubeadmConfigTemplate(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createWorkersAzureMachineTemplate(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createWorkersMachineDeployment(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// updateCRs updates existing CRs such as Cluster and AzureCluster with
// configuration that is compatible with upstream controllers.
func (m *azureMigrator) updateCRs(ctx context.Context) error {
	return nil
}

// triggerMigration executes the last missing updates on CRs so that
// reconciliation transistions to upstream controllers.
func (m *azureMigrator) triggerMigration(ctx context.Context) error {
	return nil
}
