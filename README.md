# capi-migration

## Migration process outline

### Preparation Phase

 * Create a new root CA for etcd and add it to the management cluster as a secret (according to CABPK conventions)
 * Roll the existing masters with a CA bundle of the old and new CA
 * Export the root (cert and key) from vault and store it in a secret on the management cluster

### Migration Phase

 * Migrate the CRs
 * Once the CRs are ready hand them over to the CAPI controllers
 * New nodes will be created
 * Drain and remove the old master once the new one is there
 * Edit the coredns deployment to fix the volume definition (not sure why it's broken)
 * drain and remove old node pool

### Cleanup Phase

 * Remove the old CA from the etcd bundle
 * Roll the masters again

Errors still to be solved:

 * externalDNS crashes
worker nodes still have useManagedIdentity set to false despite the AzureMachineTemplate having it set to SystemAssigned  (this is likely the cause for external-dns crash listed above)
 * PVC are not being provisioned
 * Load balancer has issues
