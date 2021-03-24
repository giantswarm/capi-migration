package migration

import (
	"context"
	"fmt"

	provider "github.com/giantswarm/apiextensions/v3/pkg/apis/provider/v1alpha1"
	release "github.com/giantswarm/apiextensions/v3/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/apiextensions/v3/pkg/label"
	"github.com/giantswarm/k8sclient/v4/pkg/k8sclient"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/giantswarm/tenantcluster/v3/pkg/tenantcluster"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	capz "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha3"
	capzexp "sigs.k8s.io/cluster-api-provider-azure/exp/api/v1alpha3"
	"sigs.k8s.io/cluster-api/api/v1alpha3"
	capi "sigs.k8s.io/cluster-api/api/v1alpha3"
	kubeadm "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	capiexp "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

type AzureMigrationConfig struct {
	// Migration configuration + dependencies such as k8s client.
	CtrlClient    ctrl.Client
	Logger        micrologger.Logger
	TenantCluster tenantcluster.Interface
}

type azureMigratorFactory struct {
	config AzureMigrationConfig
}

type azureCRs struct {
	encryptionSecret *corev1.Secret
	azureConfig      *provider.AzureConfig
	release          *release.Release

	cluster             *capi.Cluster
	azureCluster        *capz.AzureCluster
	kubeadmControlPlane *kubeadm.KubeadmControlPlane

	machinePools      []capiexp.MachinePool
	azureMachinePools []capzexp.AzureMachinePool
}

type azureMigrator struct {
	clusterID string

	crs azureCRs

	// Migration configuration, dependencies + intermediate cache for involved
	// CRs.
	logger       micrologger.Logger
	mcCtrlClient ctrl.Client
	wcCtrlClient ctrl.Client
}

func NewAzureMigratorFactory(cfg AzureMigrationConfig) (MigratorFactory, error) {
	return &azureMigratorFactory{
		config: cfg,
	}, nil
}

func (f *azureMigratorFactory) NewMigrator(cluster *v1alpha3.Cluster) (Migrator, error) {
	url := fmt.Sprintf("%s:%d", cluster.Spec.ControlPlaneEndpoint.Host, cluster.Spec.ControlPlaneEndpoint.Port)
	restConfig, err := f.config.TenantCluster.NewRestConfig(context.Background(), cluster.Name, url)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	k8sClient, err := k8sclient.NewClients(k8sclient.ClientsConfig{
		Logger:     f.config.Logger,
		RestConfig: rest.CopyConfig(restConfig),
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &azureMigrator{
		clusterID: cluster.Name,
		// rest of the config from f.config...
		logger:       f.config.Logger,
		mcCtrlClient: f.config.CtrlClient,
		wcCtrlClient: k8sClient.CtrlClient(),
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

	err = m.stopOldMasterComponents(ctx)
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

	releaseVer := m.crs.cluster.GetLabels()[label.ReleaseVersion]
	err = m.readRelease(ctx, releaseVer)
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
	var err error

	err = m.updateCluster(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.updateAzureCluster(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// triggerMigration executes the last missing updates on CRs so that
// reconciliation transistions to upstream controllers.
func (m *azureMigrator) triggerMigration(ctx context.Context) error {
	return nil
}
