package key

import "fmt"

func AzureMasterVMSSName(clusterID string) string {
	return fmt.Sprintf("%s-master-%s", clusterID, clusterID)
}
