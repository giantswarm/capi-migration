![CI](https://github.com/giantswarm/capi-migration/actions/workflows/ci.yaml/badge.svg)

# capi-migration

- [Migration process outline](#migration-process-outline)
- [Development](#development)

## Migration process outline

### Preparation Phase

 * Create a new root CA for etcd and add it to the management cluster as a secret (according to CABPK conventions)
 * Roll the existing masters with a CA bundle of the old and new CA
 * Export the root (cert and key) from vault and store it in a secret on the management cluster
 * Disable the old controller-managers (new nodes will not be able to join otherwise)
 * Disable the old api-server (as soon as the local etcd instance is removed from the etcd cluster, it will fail because it can't connect to etcd any more. This causes the API service to be down even if the new API server instance is running).

### Migration Phase

 * Migrate the CRs
 * Once the CRs are ready - hand them over to the CAPI controllers
 * New nodes will be created
 * Drain and remove the old master once the new one is there
 * Edit the coredns deployment to fix the volume definition (not sure why it's broken)
 * drain and remove old node pool

### Cleanup Phase

 * Remove the old CA from the etcd bundle
 * Roll the masters again

### Errors still to be solved

 * externalDNS crashes
 * worker nodes still have `useManagedIdentity` set to `false` despite the `AzureMachineTemplate` having it set to `SystemAssigned`  (this is likely the cause for external-dns crash listed above)
 * PVC are not being provisioned
 * Load balancer has issues (might be related to https://github.com/kubernetes-sigs/cloud-provider-azure/issues/363)

## Development

### Running locally

To try things quickly you can run `make run`. That will run `main.go` against
a current kubectl context (i.e. `kubectl config current-context`).

To make it work you need to export vault credentials:

```sh
export VAULT_ADDR="https://..."
export VAULT_TOKEN="..."
export VAULT_CAPATH="/..."

make run
```

### Deploying dev version with kustomize

To deploy a development version to a running cluster you can use `make deploy`
but **it requires some prior preparation**. You need to create
a `config/dev/manager_patch.yaml` file. There is an example file available in
`config/dev`. This file must be crafted specifically for the installation. Your
current `$USER` will be added as a suffix to all generated resources and they
will be deployed to `giantswarm` namespace. You can change the suffix with
`NAME_SUFFIX` env var. E.g. `NAME_SUFFIX=$USER make deploy`.

### Helm chart

The helm chart templates are generated using kustomize overlay stored in
`/config/helm`. To re-generate templates run `make manifests`.

To add a new configuration value:

1. Add a new flag in `main.go`. Bind the flag and bind the flag value.
2. Add a new key (with the name equal to the new flag name) to either
   `config/helm/config.yaml` or `config/helm/secret.yaml`.
3. Add a new key in `helm/*/values.yaml` that is used as a value for the key in
   CM/Secret.
4. Regenerate templates using `make manifests`.
