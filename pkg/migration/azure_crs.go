package migration

import (
	"context"
	"fmt"

	provider "github.com/giantswarm/apiextensions/v3/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"
	capz "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha3"
	capzexp "sigs.k8s.io/cluster-api-provider-azure/exp/api/v1alpha3"
	capi "sigs.k8s.io/cluster-api/api/v1alpha3"
	capiexp "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

func (m *azureMigrator) readAzureConfig(ctx context.Context) error {
	objList := &provider.AzureConfigList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.ctrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	if len(objList.Items) == 0 {
		return microerror.Mask(fmt.Errorf("AzureConfig not found for %q", m.clusterID))
	}

	obj := objList.Items[0]
	m.crs[obj.Kind] = &obj

	return nil
}

func (m *azureMigrator) readCluster(ctx context.Context) error {
	objList := &capi.ClusterList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.ctrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	if len(objList.Items) == 0 {
		return microerror.Mask(fmt.Errorf("Cluster not found for %q", m.clusterID))
	}

	obj := objList.Items[0]
	m.crs[obj.Kind] = &obj

	return nil
}

func (m *azureMigrator) readAzureCluster(ctx context.Context) error {
	objList := &capz.AzureClusterList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.ctrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	if len(objList.Items) == 0 {
		return microerror.Mask(fmt.Errorf("AzureCluster not found for %q", m.clusterID))
	}

	obj := objList.Items[0]
	m.crs[obj.Kind] = &obj

	return nil
}

func (m *azureMigrator) readMachinePools(ctx context.Context) error {
	objList := &capiexp.MachinePoolList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.ctrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs[objList.Kind] = objList

	return nil
}

func (m *azureMigrator) readAzureMachinePools(ctx context.Context) error {
	objList := &capzexp.AzureMachinePoolList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.ctrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs[objList.Kind] = objList

	return nil
}
