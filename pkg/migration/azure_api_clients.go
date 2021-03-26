package migration

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/giantswarm/microerror"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/cluster-api-provider-azure/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clientSecretKey = "clientSecret"
)

func (m *azureMigrator) getVMSSClient(ctx context.Context) (*compute.VirtualMachineScaleSetsClient, error) {
	azureCluster := m.crs.azureCluster

	if azureCluster.Spec.SubscriptionID == "" {
		return nil, microerror.Maskf(subscriptionIDNotSetError, "AzureCluster %s/%s didn't have the SubscriptionID field set", azureCluster.Namespace, azureCluster.Name)
	}

	if azureCluster.Spec.IdentityRef == nil {
		return nil, microerror.Maskf(identityRefNotSetError, "AzureCluster %s/%s didn't have the IdentityRef field set", azureCluster.Namespace, azureCluster.Name)
	}

	azureClusterIdentity := &v1alpha3.AzureClusterIdentity{}
	err := m.mcCtrlClient.Get(ctx, ctrl.ObjectKey{Namespace: azureCluster.Spec.IdentityRef.Namespace, Name: azureCluster.Spec.IdentityRef.Name}, azureClusterIdentity)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	secret := &v1.Secret{}
	err = m.mcCtrlClient.Get(ctx, ctrl.ObjectKey{Namespace: azureClusterIdentity.Spec.ClientSecret.Namespace, Name: azureClusterIdentity.Spec.ClientSecret.Name}, secret)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	subscriptionID := azureCluster.Spec.SubscriptionID
	clientID := azureClusterIdentity.Spec.ClientID
	tenantID := azureClusterIdentity.Spec.TenantID
	clientSecret, err := valueFromSecret(secret, clientSecretKey)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	azureClient := compute.NewVirtualMachineScaleSetsClient(subscriptionID)
	credentials := auth.NewClientCredentialsConfig(clientID, clientSecret, tenantID)
	authorizer, err := credentials.Authorizer()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	azureClient.Authorizer = authorizer

	return &azureClient, nil
}

func valueFromSecret(secret *v1.Secret, key string) (string, error) {
	v, ok := secret.Data[key]
	if !ok {
		return "", microerror.Maskf(missingValueError, key)
	}

	return string(v), nil
}
