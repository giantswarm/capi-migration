package migration

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/giantswarm/microerror"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/giantswarm/capi-migration/pkg/migration/internal/key"
)

func (m *azureMigrator) cleanup(ctx context.Context) error {
	err := m.ensureLegacyMastersAreDeleted(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.ensureLegacyNodePoolsAreDeleted(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) getVMSSClient(ctx context.Context) (*compute.VirtualMachineScaleSetsClient, error) {
	// TODO get service principal data from CRs
	subscriptionID := ""
	clientID := ""
	clientSecret := ""
	tenantID := ""

	azureClient := compute.NewVirtualMachineScaleSetsClient(subscriptionID)
	credentials := auth.NewClientCredentialsConfig(clientID, clientSecret, tenantID)
	authorizer, err := credentials.Authorizer()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	azureClient.Authorizer = authorizer

	return &azureClient, nil
}

func (m *azureMigrator) ensureLegacyMastersAreDeleted(ctx context.Context) error {
	// Ensure legacy masters VMSS exists or exit.
	vmssName := key.AzureMasterVMSSName(m.clusterID)

	vmssClient, err := m.getVMSSClient(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	_, err = vmssClient.Get(ctx, m.clusterID, vmssName)
	if IsAzureNotFound(err) {
		m.logger.Debugf(ctx, "VMSS %s not found in resource group %s", vmssName, m.clusterID)
		return nil
	}

	// Check if the new master exists and is ready or wait.
	nodes := v1.NodeList{}
	// TODO set the right label filters
	err = m.wcCtrlClient.List(ctx, &nodes, client.MatchingLabels{})
	if err != nil {
		return microerror.Mask(err)
	}

	if len(nodes.Items) == 0 {
		return microerror.Maskf(newMasterNotReadyError, "New master node was not found")
	}

	if len(nodes.Items) > 1 {
		return microerror.Maskf(tooManyMastersError, "Exactly one master node was expected to exists, %d found", len(nodes.Items))
	}

	if nodes.Items[0].Status.Phase != v1.NodeRunning {
		return microerror.Maskf(newMasterNotReadyError, "Master node %q is not ready (%q)", nodes.Items[0].Name, nodes.Items[0].Status.Phase)
	}

	m.logger.Debugf(ctx, "Deleting VMSS %q from resource group %q", vmssName, m.clusterID)

	// Delete GS master VMSS.
	_, err = vmssClient.Delete(ctx, m.clusterID, vmssName)
	if err != nil {
		return microerror.Mask(err)
	}

	m.logger.Debugf(ctx, "Deleted VMSS %q from resource group %q", vmssName, m.clusterID)

	return nil
}

func (m *azureMigrator) ensureLegacyNodePoolsAreDeleted(ctx context.Context) error {
	// Ensure there are any GS node pool VMSSes still running or exit.
	// Check if the new worker nodes exist, are ready and are the same number as GS node pools sum.
	// For each GS node pool:
	//   Ensure GS workers have termination events enabled.
	//   Delete GS workers VMSSes (1 node pool at a time).
	return nil
}
