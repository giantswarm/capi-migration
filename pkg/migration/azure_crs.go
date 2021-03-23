package migration

import (
	"bytes"
	"context"
	"fmt"
	"html/template"

	provider "github.com/giantswarm/apiextensions/v3/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capz "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha3"
	capzexp "sigs.k8s.io/cluster-api-provider-azure/exp/api/v1alpha3"
	capi "sigs.k8s.io/cluster-api/api/v1alpha3"
	cabpkv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha3"
	kubeadm "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	capiexp "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	EncryptionSecret = "EncryptionSecret"
)

func (m *azureMigrator) createEncryptionConfigSecret(ctx context.Context) error {
	var origEncryptionSecret *corev1.Secret
	{
		obj, exists := m.crs[EncryptionSecret]
		if !exists {
			return microerror.Mask(fmt.Errorf("encryption secret not found"))
		}

		origEncryptionSecret, ok := obj.(*corev1.Secret)
		if !ok {
			return microerror.Mask(fmt.Errorf("can't convert obj (%T) to %T", obj, origEncryptionSecret))
		}
	}

	encryptionConfigTmpl := `
kind: EncryptionConfiguration
apiVersion: apiserver.config.k8s.io/v1
resources:
  - resources:
    - secrets
    providers:
    - aescbc:
        keys:
        - name: key1
          secret: %s
    - identity: {}`

	renderedConfig := fmt.Sprintf(encryptionConfigTmpl, origEncryptionSecret.Data["encryption"])

	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-k8s-encryption-config", m.clusterID),
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"encryption": renderedConfig,
		},
	}

	err := m.mcCtrlClient.Create(ctx, s)
	if apierrors.IsAlreadyExists(err) {
		// It's fine. No worries.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) createProxyConfigSecret(ctx context.Context) error {
	proxyConfig := `
apiVersion: kubeproxy.config.k8s.io/v1alpha1
clientConnection:
  kubeconfig: /etc/kubernetes/config/proxy-kubeconfig.yaml
kind: KubeProxyConfiguration
mode: iptables
metricsBindAddress: 0.0.0.0:10249`

	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-proxy-config", m.clusterID),
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"proxy": proxyConfig,
		},
	}
	err := m.mcCtrlClient.Create(ctx, s)
	if apierrors.IsAlreadyExists(err) {
		// It's fine. No worries.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) createKubeadmControlPlane(ctx context.Context) error {
	tmpl, err := template.ParseFS(templatesFS, "templates/kubeadm_controlplane_azure.yaml.tmpl")
	if err != nil {
		return microerror.Mask(err)
	}

	cfg := struct {
		ClusterID              string
		ClusterCIDR            string
		ClusterMasterIP        string
		EtcdVersion            string
		K8sVersion             string
		InstallationBaseDomain string
	}{
		ClusterID:              m.clusterID,
		ClusterCIDR:            "10.2.0.0/16",
		ClusterMasterIP:        "10.2.0.4",
		EtcdVersion:            "v3.4.13",
		K8sVersion:             "v1.19.9",
		InstallationBaseDomain: "gremlin.germanywestcentral.azure.gigantic.io",
	}

	buf := bytes.NewBuffer(nil)
	err = tmpl.Execute(buf, cfg)
	if err != nil {
		return microerror.Mask(err)
	}

	kcp := &kubeadm.KubeadmControlPlane{}
	err = yaml.Unmarshal(buf.Bytes(), kcp)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.mcCtrlClient.Create(ctx, kcp)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) createMasterAzureMachineTemplate(ctx context.Context) error {
	tmpl, err := template.ParseFS(templatesFS, "templates/controlplane_azure_machine_template.yaml.tmpl")
	if err != nil {
		return microerror.Mask(err)
	}

	cfg := struct {
		ClusterID string
	}{
		ClusterID: m.clusterID,
	}

	buf := bytes.NewBuffer(nil)
	err = tmpl.Execute(buf, cfg)
	if err != nil {
		return microerror.Mask(err)
	}

	amt := &capz.AzureMachineTemplate{}
	err = yaml.Unmarshal(buf.Bytes(), amt)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.mcCtrlClient.Create(ctx, amt)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) createWorkersKubeadmConfigTemplate(ctx context.Context) error {
	tmpl, err := template.ParseFS(templatesFS, "templates/workers_kubeadm_config_template_azure.yaml.tmpl")
	if err != nil {
		return microerror.Mask(err)
	}

	cfg := struct {
		ClusterID string
	}{
		ClusterID: m.clusterID,
	}

	buf := bytes.NewBuffer(nil)
	err = tmpl.Execute(buf, cfg)
	if err != nil {
		return microerror.Mask(err)
	}

	kct := &cabpkv1.KubeadmConfigTemplate{}
	err = yaml.Unmarshal(buf.Bytes(), kct)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.mcCtrlClient.Create(ctx, kct)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) createWorkersAzureMachineTemplate(ctx context.Context) error {
	tmpl, err := template.ParseFS(templatesFS, "templates/workers_azure_machine_template.yaml.tmpl")
	if err != nil {
		return microerror.Mask(err)
	}

	cfg := struct {
		ClusterID string
	}{
		ClusterID: m.clusterID,
	}

	buf := bytes.NewBuffer(nil)
	err = tmpl.Execute(buf, cfg)
	if err != nil {
		return microerror.Mask(err)
	}

	amt := &capz.AzureMachineTemplate{}
	err = yaml.Unmarshal(buf.Bytes(), amt)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.mcCtrlClient.Create(ctx, amt)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) createWorkersMachineDeployment(ctx context.Context) error {
	tmpl, err := template.ParseFS(templatesFS, "templates/workers_machine_deployment.yaml.tmpl")
	if err != nil {
		return microerror.Mask(err)
	}

	cfg := struct {
		ClusterID  string
		K8sVersion string
	}{
		ClusterID:  m.clusterID,
		K8sVersion: "v1.19.9",
	}

	buf := bytes.NewBuffer(nil)
	err = tmpl.Execute(buf, cfg)
	if err != nil {
		return microerror.Mask(err)
	}

	md := &capi.MachineDeployment{}
	err = yaml.Unmarshal(buf.Bytes(), md)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.mcCtrlClient.Create(ctx, md)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) readEncryptionSecret(ctx context.Context) error {
	obj := &corev1.Secret{}
	key := ctrl.ObjectKey{Namespace: "default", Name: fmt.Sprintf("%s-encryption", m.clusterID)}
	err := m.mcCtrlClient.Get(ctx, key, obj)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs[EncryptionSecret] = obj

	return nil
}

func (m *azureMigrator) readAzureConfig(ctx context.Context) error {
	objList := &provider.AzureConfigList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.mcCtrlClient.List(ctx, objList, selector)
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
	err := m.mcCtrlClient.List(ctx, objList, selector)
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
	err := m.mcCtrlClient.List(ctx, objList, selector)
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
	err := m.mcCtrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs[objList.Kind] = objList

	return nil
}

func (m *azureMigrator) readAzureMachinePools(ctx context.Context) error {
	objList := &capzexp.AzureMachinePoolList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.mcCtrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs[objList.Kind] = objList

	return nil
}
