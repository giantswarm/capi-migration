package migration

import (
	"context"
	"fmt"
	"strings"

	giantswarmawsalpha3 "github.com/giantswarm/apiextensions/v3/pkg/apis/infrastructure/v1alpha2"
	release "github.com/giantswarm/apiextensions/v3/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/apiextensions/v3/pkg/label"
	"github.com/giantswarm/microerror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capa "sigs.k8s.io/cluster-api-provider-aws/api/v1alpha3"
	capaexp "sigs.k8s.io/cluster-api-provider-aws/exp/api/v1alpha3"
	capi "sigs.k8s.io/cluster-api/api/v1alpha3"
	cabpkv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha3"
	kubeadm "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	capiexp "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

func (m *awsMigrator) createEncryptionConfigSecret(ctx context.Context) error {
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

func (m *awsMigrator) createProxyConfigSecret(ctx context.Context) error {
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

func (m *awsMigrator) createKubeadmControlPlane(ctx context.Context) error {
	// TODO

	kcp := &kubeadm.KubeadmControlPlane{}
	err := m.mcCtrlClient.Create(ctx, kcp)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	// Store control plane CR for later referencing into Cluster CR.
	m.crs.kubeadmControlPlane = kcp

	return nil
}

func (m *awsMigrator) createMasterAWSMachineTemplate(ctx context.Context) error {
	// TODO

	amt := &capa.AWSMachineTemplate{}
	err := m.mcCtrlClient.Create(ctx, amt)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *awsMigrator) createWorkersKubeadmConfigTemplate(ctx context.Context) error {
	// TODO

	kct := &cabpkv1.KubeadmConfigTemplate{}

	err := m.mcCtrlClient.Create(ctx, kct)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *awsMigrator) createWorkersAWSMachineTemplate(ctx context.Context) error {
	// TODO

	amt := &capaexp.AWSMachinePool{}

	err := m.mcCtrlClient.Create(ctx, amt)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *awsMigrator) createWorkersMachinePools(ctx context.Context) error {
	// TODO
	md := &capi.MachineDeployment{}

	err := m.mcCtrlClient.Create(ctx, md)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *awsMigrator) readEncryptionSecret(ctx context.Context) error {
	obj := &corev1.Secret{}
	key := ctrl.ObjectKey{Namespace: "default", Name: fmt.Sprintf("%s-encryption", m.clusterID)}
	err := m.mcCtrlClient.Get(ctx, key, obj)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs.encryptionSecret = obj

	return nil
}

func (m *awsMigrator) readCluster(ctx context.Context) error {
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

func (m *awsMigrator) readAWSCluster(ctx context.Context) error {
	objList := &giantswarmawsalpha3.AWSClusterList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.mcCtrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	if len(objList.Items) == 0 {
		return microerror.Mask(fmt.Errorf("AWSCluster not found for %q", m.clusterID))
	}

	if len(objList.Items) > 1 {
		return microerror.Mask(fmt.Errorf("more than one AWSCluster for cluster ID %q", m.clusterID))
	}

	obj := objList.Items[0]
	m.crs.awsCluster = &obj

	return nil
}

func (m *awsMigrator) readMachinePools(ctx context.Context) error {
	objList := &capiexp.MachinePoolList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.mcCtrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs.machinePools = objList.Items

	return nil
}

func (m *awsMigrator) readAWSMachineDeployments(ctx context.Context) error {
	objList := &giantswarmawsalpha3.AWSMachineDeploymentList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.mcCtrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs.awsMachinePools = objList.Items

	return nil
}

func (m *awsMigrator) readRelease(ctx context.Context, ver string) error {
	ver = strings.TrimPrefix(ver, "v")
	r := &release.Release{}
	err := m.mcCtrlClient.Get(ctx, ctrl.ObjectKey{Name: ver}, r)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs.release = r

	return nil
}

func (m *awsMigrator) updateCluster(ctx context.Context) error {
	cluster := m.crs.cluster

	// Drop operator version label.
	delete(cluster.Labels, label.AWSOperatorVersion)

	// Drop finalizers.
	cluster.Finalizers = nil

	// TODO

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

func (m *awsMigrator) updateAWSCluster(ctx context.Context) error {
	cluster := m.crs.awsCluster

	// Drop operator version label.
	delete(cluster.Labels, label.AWSOperatorVersion)

	// Drop finalizers.
	cluster.Finalizers = nil

	// TODO

	err := m.mcCtrlClient.Update(ctx, cluster)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
