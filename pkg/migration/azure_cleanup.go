package migration

import (
	"context"

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
	{
		nodes := v1.NodeList{}
		err = m.wcCtrlClient.List(ctx, &nodes, client.MatchingLabels{"node-role.kubernetes.io/master": ""})
		if err != nil {
			return microerror.Mask(err)
		}

		// Filter out nodes having label "role=master".
		var newMasters []v1.Node
		for _, n := range nodes.Items {
			if n.Labels["role"] == "master" {
				// Legacy master node.
			} else {
				newMasters = append(newMasters, n)
			}
		}

		if len(newMasters) == 0 {
			return microerror.Maskf(newMasterNotReadyError, "New master node was not found")
		}

		if len(newMasters) > 1 {
			return microerror.Maskf(tooManyMastersError, "Exactly one master node was expected to exist, %d found", len(nodes.Items))
		}

		if nodes.Items[0].Status.Phase != v1.NodeRunning {
			return microerror.Maskf(newMasterNotReadyError, "Master node %q is not ready (%q)", nodes.Items[0].Name, nodes.Items[0].Status.Phase)
		}
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
