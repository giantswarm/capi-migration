package key

import "fmt"

func AzureMasterVMSSName(clusterID string) string {
	return fmt.Sprintf("%s-master-%s", clusterID, clusterID)
}

func AzureNodePoolVMSSName(nodePoolID string) string {
	return fmt.Sprintf("nodepool-%s", nodePoolID)
}
