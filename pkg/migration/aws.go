package migration

import (
	"context"
	"fmt"

	giantswarmawsalpha3 "github.com/giantswarm/apiextensions/v3/pkg/apis/infrastructure/v1alpha2"
	release "github.com/giantswarm/apiextensions/v3/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/apiextensions/v3/pkg/label"
	"github.com/giantswarm/k8sclient/v4/pkg/k8sclient"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/giantswarm/tenantcluster/v3/pkg/tenantcluster"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/cluster-api/api/v1alpha3"
	capi "sigs.k8s.io/cluster-api/api/v1alpha3"
	kubeadm "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	capiexp "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

type AWSMigrationConfig struct {

	// Migration configuration + dependencies such as k8s client.
	AWSCredentials AWSConfig
	CtrlClient     ctrl.Client
	Logger         micrologger.Logger
	TenantCluster  tenantcluster.Interface
}

type awsMigratorFactory struct {
	config AWSMigrationConfig
}

type awsCRs struct {
	encryptionSecret *corev1.Secret
	release          *release.Release

	cluster             *capi.Cluster
	awsCluster          *giantswarmawsalpha3.AWSCluster
	kubeadmControlPlane *kubeadm.KubeadmControlPlane

	machinePools    []capiexp.MachinePool
	awsMachinePools []giantswarmawsalpha3.AWSMachineDeployment
}

type awsMigrator struct {
	awsClients     *awsClients
	awsCredentials AWSConfig
	clusterID      string

	crs awsCRs

	// Migration configuration, dependencies + intermediate cache for involved
	// CRs.
	logger       micrologger.Logger
	mcCtrlClient ctrl.Client
	wcCtrlClient ctrl.Client
}

func NewAWSMigratorFactory(cfg AWSMigrationConfig) (MigratorFactory, error) {
	return &awsMigratorFactory{
		config: cfg,
	}, nil
}

func (f *awsMigratorFactory) NewMigrator(cluster *v1alpha3.Cluster) (Migrator, error) {
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

	return &awsMigrator{
		awsCredentials: f.config.AWSCredentials,
		clusterID:      cluster.Name,

		// rest of the config from f.config...
		logger:       f.config.Logger,
		mcCtrlClient: f.config.CtrlClient,
		wcCtrlClient: k8sClient.CtrlClient(),
	}, nil
}

func (m *awsMigrator) IsMigrated(ctx context.Context) (bool, error) {
	return false, nil
}

func (m *awsMigrator) IsMigrating(ctx context.Context) (bool, error) {
	return false, nil
}

func (m *awsMigrator) Prepare(ctx context.Context) error {
	var err error

	err = m.migrateCertsSecrets(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

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

	// need to figure out how to make run for both providers
	//err = m.stopOldMasterComponents(ctx)
	//if err != nil {
	//	return microerror.Mask(err)
	//}

	return nil
}

func (m *awsMigrator) TriggerMigration(ctx context.Context) error {
	err := m.triggerMigration(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *awsMigrator) Cleanup(ctx context.Context) error {
	migrated, err := m.IsMigrated(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	if !migrated {
		return fmt.Errorf("cluster has not migrated yet")
	}

	return nil
}

// readCRs reads existing CRs involved in migration. For AWS this contains
// roughly following CRs:
// - Cluster
// - AWSCluster
// - MachinePools
// - AWSMachineDeployments
//
func (m *awsMigrator) readCRs(ctx context.Context) error {
	var err error

	err = m.readEncryptionSecret(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.readCluster(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.readAWSCluster(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.readMachinePools(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.readAWSMachineDeployments(ctx)
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
// AWSMachineTemplate for new master nodes.
func (m *awsMigrator) prepareMissingCRs(ctx context.Context) error {
	var err error

	err = m.createEncryptionConfigSecret(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createCustomFilesSecret(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createKubeadmControlPlane(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createMasterAWSMachineTemplate(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createWorkersKubeadmConfigTemplate(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createWorkersAWSMachineTemplate(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.createWorkersMachinePools(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// updateCRs updates existing CRs such as Cluster and AWSCluster with
// configuration that is compatible with upstream controllers.
func (m *awsMigrator) updateCRs(ctx context.Context) error {
	var err error

	err = m.updateCluster(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.updateAWSCluster(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// triggerMigration executes the last missing updates on CRs so that
// reconciliation transistions to upstream controllers.
func (m *awsMigrator) triggerMigration(ctx context.Context) error {
	return nil
}
