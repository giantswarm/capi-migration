package migration

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"text/template"

	"github.com/Azure/go-autorest/autorest/to"
	provider "github.com/giantswarm/apiextensions/v3/pkg/apis/provider/v1alpha1"
	release "github.com/giantswarm/apiextensions/v3/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/apiextensions/v3/pkg/label"
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

	renderedConfig := fmt.Sprintf(encryptionConfigTmpl, m.crs.encryptionSecret.Data["encryption"])

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

	baseDomain, err := getInstallationBaseDomainFromAPIEndpoint(m.crs.azureCluster.Spec.ControlPlaneEndpoint.Host)
	if err != nil {
		return microerror.Mask(err)
	}

	vnet, err := m.getVNETCIDR()
	if err != nil {
		return microerror.Mask(err)
	}

	releaseComponents := getReleaseComponents(m.crs.release)

	cfg := map[string]string{
		"ClusterID":              m.clusterID,
		"ClusterCIDR":            vnet.String(),
		"ClusterMasterIP":        getMasterIPForVNet(vnet).String(),
		"EtcdVersion":            releaseComponents["etcd"],
		"K8sVersion":             releaseComponents["kubernetes"],
		"InstallationBaseDomain": baseDomain,
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

	// Store control plane CR for later referencing into Cluster CR.
	m.crs.kubeadmControlPlane = kcp

	return nil
}

func (m *azureMigrator) createMasterAzureMachineTemplate(ctx context.Context) error {
	tmpl, err := template.ParseFS(templatesFS, "templates/controlplane_azure_machine_template.yaml.tmpl")
	if err != nil {
		return microerror.Mask(err)
	}

	cfg := map[string]string{
		"ClusterID":     m.clusterID,
		"AzureLocation": m.crs.azureCluster.Spec.Location,
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

	cfg := map[string]string{
		"ClusterID": m.clusterID,
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

	cfg := map[string]string{
		"ClusterID": m.clusterID,
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

	cfg := map[string]string{
		"ClusterID":  m.clusterID,
		"K8sVersion": "v1.19.9",
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

	m.crs.encryptionSecret = obj

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

	if len(objList.Items) > 1 {
		return microerror.Mask(fmt.Errorf("more than one AzureConfig for cluster ID %q", m.clusterID))
	}

	obj := objList.Items[0]
	m.crs.azureConfig = &obj

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

	if len(objList.Items) > 1 {
		return microerror.Mask(fmt.Errorf("more than one Cluster for cluster ID %q", m.clusterID))
	}

	obj := objList.Items[0]
	m.crs.cluster = &obj

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

	if len(objList.Items) > 1 {
		return microerror.Mask(fmt.Errorf("more than one AzureCluster for cluster ID %q", m.clusterID))
	}

	obj := objList.Items[0]
	m.crs.azureCluster = &obj

	return nil
}

func (m *azureMigrator) readMachinePools(ctx context.Context) error {
	objList := &capiexp.MachinePoolList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.mcCtrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs.machinePools = objList.Items

	return nil
}

func (m *azureMigrator) readAzureMachinePools(ctx context.Context) error {
	objList := &capzexp.AzureMachinePoolList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.mcCtrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs.azureMachinePools = objList.Items

	return nil
}

func (m *azureMigrator) readRelease(ctx context.Context, ver string) error {
	ver = strings.TrimPrefix(ver, "v")
	r := &release.Release{}
	err := m.mcCtrlClient.Get(ctx, ctrl.ObjectKey{Name: ver}, r)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs.release = r

	return nil
}

func (m *azureMigrator) updateCluster(ctx context.Context) error {
	cluster := m.crs.cluster

	// Drop operator version label.
	delete(cluster.Labels, label.AzureOperatorVersion)

	// Drop finalizers.
	cluster.Finalizers = nil

	// Adjust k8s apiserver bind port to match kubeadm.
	if cluster.Spec.ClusterNetwork != nil && cluster.Spec.ClusterNetwork.APIServerPort != nil {
		cluster.Spec.ClusterNetwork.APIServerPort = to.Int32Ptr(6443)
	}

	cluster.Spec.ControlPlaneRef = &corev1.ObjectReference{
		APIVersion: m.crs.kubeadmControlPlane.APIVersion,
		Kind:       m.crs.kubeadmControlPlane.Kind,
		Name:       m.crs.kubeadmControlPlane.Name,
	}

	err := m.mcCtrlClient.Update(ctx, cluster)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) updateAzureCluster(ctx context.Context) error {
	cluster := m.crs.azureCluster

	// Drop operator version label.
	delete(cluster.Labels, label.AzureOperatorVersion)

	// Drop finalizers.
	cluster.Finalizers = nil

	// Use default credentials.
	cluster.Spec.IdentityRef = nil

	if cluster.Spec.NetworkSpec.APIServerLB.Name == "" {
		cluster.Spec.NetworkSpec.APIServerLB = capz.LoadBalancerSpec{
			Name: fmt.Sprintf("%s-%s-%s", cluster.Name, "API", "PublicLoadBalancer"),
			SKU:  "Standard",
			Type: "Public",
			FrontendIPs: []capz.FrontendIP{
				{
					Name: fmt.Sprintf("%s-%s-%s-%s", cluster.Name, "API", "PublicLoadBalancer", "Frontend"),
					PublicIP: &capz.PublicIPSpec{
						Name: fmt.Sprintf("%s-%s-%s-%s", cluster.Name, "API", "PublicLoadBalancer", "PublicIP"),
					},
				},
			},
		}
	}

	// START OF UGLY HACK **************************************************
	// XXX: This is just a shortcut to make testing ergonomics better. Final
	// code should be rewritten.
	if len(cluster.Spec.NetworkSpec.APIServerLB.FrontendIPs) == 0 {
		ip := capz.FrontendIP{
			Name: fmt.Sprintf("%s-%s-%s-%s", cluster.Name, "API", "PublicLoadBalancer", "Frontend"),
			PublicIP: &capz.PublicIPSpec{
				Name: fmt.Sprintf("%s-%s-%s-%s", cluster.Name, "API", "PublicLoadBalancer", "PublicIP"),
			},
		}

		cluster.Spec.NetworkSpec.APIServerLB.FrontendIPs = append(cluster.Spec.NetworkSpec.APIServerLB.FrontendIPs, ip)
	}

	if cluster.Spec.NetworkSpec.APIServerLB.FrontendIPs[0].PublicIP == nil {
		cluster.Spec.NetworkSpec.APIServerLB.FrontendIPs[0].PublicIP = &capz.PublicIPSpec{
			Name: fmt.Sprintf("%s-%s-%s-%s", cluster.Name, "API", "PublicLoadBalancer", "PublicIP"),
		}
	}

	if cluster.Spec.NetworkSpec.APIServerLB.FrontendIPs[0].PublicIP.Name == "" {
		cluster.Spec.NetworkSpec.APIServerLB.FrontendIPs[0].PublicIP.Name = fmt.Sprintf("%s-%s-%s-%s", cluster.Name, "API", "PublicLoadBalancer", "PublicIP")
	}
	// END OF UGLY HACK **************************************************

	var masterSubnetFound, workerSubnetFound bool
	for _, snet := range cluster.Spec.NetworkSpec.Subnets {
		if strings.HasSuffix(snet.Name, "VirtualNetwork-MasterSubnet") {
			masterSubnetFound = true
		}
		if strings.HasSuffix(snet.Name, "VirtualNetwork-WorkerSubnet") {
			workerSubnetFound = true
		}
	}

	if !masterSubnetFound && !workerSubnetFound && len(cluster.Spec.NetworkSpec.Subnets) > 0 {
		// When there's no pre-built master nor legacy worker subnet, but the
		// subnet array still has items, it means there are node pool subnets
		// for worker nodes and hence there's no need to inject legacy worker
		// subnet.
		workerSubnetFound = true
	}

	_, vnet, err := net.ParseCIDR(cluster.Spec.NetworkSpec.Vnet.CIDRBlocks[0])
	if err != nil {
		return microerror.Mask(err)
	}

	if !masterSubnetFound {
		masterSubnetCIDR := &net.IPNet{
			IP:   vnet.IP,
			Mask: net.IPv4Mask(255, 255, 255, 0),
		}

		s := &capz.SubnetSpec{
			Name: fmt.Sprintf("%s-VirtualNetwork-MasterSubnet", cluster.Name),
			CIDRBlocks: []string{
				masterSubnetCIDR.String(),
			},
			Role: capz.SubnetControlPlane,
		}

		cluster.Spec.NetworkSpec.Subnets = append(cluster.Spec.NetworkSpec.Subnets, s)
	}

	if !workerSubnetFound {
		n := vnet.IP.To4()
		if n == nil {
			return microerror.Mask(fmt.Errorf("VNET CIDR %q is not an IPv4 address", cluster.Spec.NetworkSpec.Vnet.CIDRBlocks[0]))
		}

		// Bump up third octet by one to get first worker subnet.
		workerIP := net.IPv4(n[0], n[1], n[2]+1, n[3])
		workerSubnetCIDR := &net.IPNet{
			IP:   workerIP,
			Mask: net.IPv4Mask(255, 255, 255, 0),
		}
		s := &capz.SubnetSpec{
			Name: fmt.Sprintf("%s-VirtualNetwork-WorkerSubnet", cluster.Name),
			CIDRBlocks: []string{
				workerSubnetCIDR.String(),
			},
			Role: capz.SubnetNode,
		}

		cluster.Spec.NetworkSpec.Subnets = append(cluster.Spec.NetworkSpec.Subnets, s)
	}

	err = m.mcCtrlClient.Update(ctx, cluster)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *azureMigrator) getVNETCIDR() (*net.IPNet, error) {
	if len(m.crs.azureCluster.Spec.NetworkSpec.Vnet.CIDRBlocks) == 0 {
		return nil, microerror.Mask(fmt.Errorf("VNET CIDR not found for %q", m.clusterID))
	}

	_, n, err := net.ParseCIDR(m.crs.azureCluster.Spec.NetworkSpec.Vnet.CIDRBlocks[0])
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return n, nil
}

func getReleaseComponents(r *release.Release) map[string]string {
	components := make(map[string]string)
	for _, c := range r.Spec.Components {
		components[c.Name] = c.Version
	}

	return components
}

func getInstallationBaseDomainFromAPIEndpoint(apiEndpoint string) (string, error) {
	labels := strings.Split(apiEndpoint, ".")

	for i, l := range labels {
		if l == "k8s" {
			return strings.Join(labels[i+1:], "."), nil
		}
	}

	return "", microerror.Mask(fmt.Errorf("can't find domain label 'k8s' from ControlPlaneEndpoint.Host"))
}

func getMasterIPForVNet(vnet *net.IPNet) net.IP {
	ip := vnet.IP.To4()
	if ip == nil {
		// We don't have IPv6. This is fine. Makes API more convenient.
		panic("VNET CIDR is IPv6")
	}

	return net.IPv4(ip[0], ip[1], ip[2], ip[3]+4)
}
