package migration

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
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
	bootstrap "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha3"
	bootstraptypes "sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta1"
	kubeadm "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	capiexp "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/giantswarm/capi-migration/pkg/migration/internal/key"
	"github.com/giantswarm/capi-migration/pkg/migration/templates"
)

const (
	joinEtcdClusterScriptKey = "join-etcd-cluster"
	encryptionKeyKey         = "encryption"
	kubeProxyConfigKey       = "kubeproxy-config"
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

func (m *awsMigrator) createCustomFilesSecret(ctx context.Context) error {
	namespace := "default"
	params := templates.CustomFilesParams{
		APIEndpoint:  key.AWSAPIEndpointFromDomain(m.crs.awsCluster.Spec.Cluster.DNS.Domain, m.clusterID),
		ETCDEndpoint: key.AWSEtcdEndpointFromDomain(m.crs.awsCluster.Spec.Cluster.DNS.Domain, m.clusterID),
	}

	joinEtcdClusterContent, err := templates.RenderTemplate(templates.AWSJoinCluster, params)
	if err != nil {
		return microerror.Mask(err)
	}

	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.AWSCustomFilesSecretName(m.clusterID),
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			joinEtcdClusterScriptKey: joinEtcdClusterContent,
			kubeProxyConfigKey:       templates.KubeProxyConfig,
		},
	}
	err = m.mcCtrlClient.Create(ctx, s)
	if apierrors.IsAlreadyExists(err) {
		// It's fine. No worries.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *awsMigrator) createKubeadmControlPlane(ctx context.Context) error {
	replicas := int32(1)
	releaseComponents := getReleaseComponents(m.crs.release)

	kcp := &kubeadm.KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.AWSKubeadmControlPlaneName(m.clusterID),
			Namespace: m.crs.g8sControlPlane.Namespace,
		},
		Spec: kubeadm.KubeadmControlPlaneSpec{
			InfrastructureTemplate: corev1.ObjectReference{
				APIVersion: capa.GroupVersion.String(),
				Name:       key.AWSMachineTemplateNameForCP(m.clusterID),
				Kind:       "AWSMachineTemplate",
			},
			KubeadmConfigSpec: bootstrap.KubeadmConfigSpec{
				ClusterConfiguration: &bootstraptypes.ClusterConfiguration{
					APIServer: bootstraptypes.APIServer{
						ControlPlaneComponent: bootstraptypes.ControlPlaneComponent{
							ExtraArgs: map[string]string{
								"cloud-provider":             "aws",
								"etcd-prefix":                "giantswarm.io",
								"encryption-provider-config": "/etc/kubernetes/encryption/k8s-encryption-config.yaml",
							},
							ExtraVolumes: []bootstraptypes.HostPathMount{
								{
									Name:      "encryption",
									HostPath:  "/etc/kubernetes/encryption/",
									MountPath: "/etc/kubernetes/encryption/",
								},
							},
						},
						CertSANs: []string{
							key.AWSAPIEndpointFromDomain(m.crs.awsCluster.Spec.Cluster.DNS.Domain, m.clusterID),
						},
					},
					ControllerManager: bootstraptypes.ControlPlaneComponent{
						ExtraArgs: map[string]string{
							"cloud-provider": "aws",
						},
					},
					Etcd: bootstraptypes.Etcd{
						Local: &bootstraptypes.LocalEtcd{
							DataDir: "/var/lib/etcd/data",
							ExtraArgs: map[string]string{
								"initial-cluster-state":                          "existing",
								"initial-cluster":                                "$ETCD_INITIAL_CLUSTER",
								"experimental-peer-skip-client-san-verification": "true",
							},
						},
					},
				},
				InitConfiguration: &bootstraptypes.InitConfiguration{
					NodeRegistration: bootstraptypes.NodeRegistrationOptions{
						KubeletExtraArgs: map[string]string{
							"cloud-provider": "aws",
						},
						Name: "{{ ds.meta_data.local_hostname }}",
					},
					LocalAPIEndpoint: bootstraptypes.APIEndpoint{
						BindPort: 443,
					},
				},
				JoinConfiguration: &bootstraptypes.JoinConfiguration{
					NodeRegistration: bootstraptypes.NodeRegistrationOptions{
						KubeletExtraArgs: map[string]string{
							"cloud-provider": "aws",
						},
						Name: "{{ ds.meta_data.local_hostname }}",
					},
				},
				Files: []bootstrap.File{
					{
						Path:  "/migration/join-existing-cluster.sh",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.AWSCustomFilesSecretName(m.clusterID),
								Key:  joinEtcdClusterScriptKey,
							},
						},
					},
					{
						Path:  "/etc/kubernetes/encryption/k8s-encryption-config.yaml",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.EncryptionConfigSecretName(m.clusterID),
								Key:  encryptionKeyKey,
							},
						},
					},
					{
						Path:  "/etc/kubernetes/config/proxy-config.yml",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.AWSCustomFilesSecretName(m.clusterID),
								Key:  kubeProxyConfigKey,
							},
						},
					},
					{
						Path:  "/etc/kubernetes/pki/ca.crt",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.CACertsSecretName(m.clusterID),
								Key:  "tls.crt",
							},
						},
					},
					{
						Path:  "/etc/kubernetes/pki/ca.key",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.CACertsSecretName(m.clusterID),
								Key:  "tls.key",
							},
						},
					},
					{
						Path:  "/etc/kubernetes/pki/etcd/ca.key",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.CACertsSecretName(m.clusterID),
								Key:  "tls.key",
							},
						},
					},
					{
						Path:  "/etc/kubernetes/pki/etcd/ca.crt",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.CACertsSecretName(m.clusterID),
								Key:  "tls.crt",
							},
						},
					},
					{
						Path:  "/etc/kubernetes/pki/sa.pub",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.SACertsSecretName(m.clusterID),
								Key:  "tls.crt",
							},
						},
					},
					{
						Path:  "/etc/kubernetes/pki/sa.key",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.SACertsSecretName(m.clusterID),
								Key:  "tls.key",
							},
						},
					},
					{
						Path:  "/etc/kubernetes/pki/etcd/old.key",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.EtcdCertsSecretName(m.clusterID),
								Key:  "key",
							},
						},
					},
					{
						Path:  "/etc/kubernetes/pki/etcd/old.crt",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.EtcdCertsSecretName(m.clusterID),
								Key:  "crt",
							},
						},
					},
				},
				PreKubeadmCommands: []string{
					"hostnamectl set-hostname $(curl http://169.254.169.254/latest/meta-data/local-hostname) # set proper hostname - necessary for kubeProxy to detect node name",
					"iptables -A PREROUTING -t nat  -p tcp --dport 6443 -j REDIRECT --to-port 443 # route traffic from 6443 to 443",
					"/bin/sh /migration/join-existing-cluster.sh",
				},
				Users: []bootstrap.User{
					{
						Name: "calvix",
						SSHAuthorizedKeys: []string{
							"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC9IyAZvlEL7lrxDghpqWjs/z/q4E0OtEbmKW9oD0zhYfyHIaX33YYoj3iC7oEd6OEvY4+L4awjRZ2FrXerN/tTg9t1zrW7f7Tah/SnS9XYY9zyo4uzuq1Pa6spOkjpcjtXbQwdQSATD0eeLraBWWVBDIg1COAMsAhveP04UaXAKGSQst6df007dIS5pmcATASNNBc9zzBmJgFwPDLwVviYqoqcYTASka4fSQhQ+fSj9zO1pgrCvvsmA/QeHz2Cn5uFzjh8ftqkM10sjiYibknsBuvVKZ2KpeTY6XoTOT0d9YWoJpfqAEE00+RmYLqDTQGWm5pRuZSc9vbnnH2MiEKf calvix@xxxx",
						},
					},
				},
			},
			Replicas: &replicas,
			Version:  releaseComponents["K8sVersion"],
		},
	}

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
	var masterSecurityGroupID *string
	{
		i := &ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("tag:Name"),
					Values: aws.StringSlice([]string{fmt.Sprintf("%s-master", m.clusterID)}),
				},
			},
		}
		o, err := m.awsClients.ec2Client.DescribeSecurityGroups(i)
		if err != nil {
			return microerror.Mask(err)
		}
		if len(o.SecurityGroups) != 1 {
			return microerror.Maskf(nil, "expected 1 master security group but found %d", len(o.SecurityGroups))
		}
		masterSecurityGroupID = o.SecurityGroups[0].GroupId
	}

	machineTemplate := &capa.AWSMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.AWSMachineTemplateNameForCP(m.clusterID),
			Namespace: m.crs.awsControlPlane.Namespace,
		},
		Spec: capa.AWSMachineTemplateSpec{
			Template: capa.AWSMachineTemplateResource{
				Spec: capa.AWSMachineSpec{
					IAMInstanceProfile: "control-plane.cluster-api-provider-aws.sigs.k8s.io",
					InstanceType:       m.crs.awsControlPlane.Spec.InstanceType,
					SSHKeyName:         aws.String("vaclav"),
					AdditionalSecurityGroups: []capa.AWSResourceReference{
						{
							ID: masterSecurityGroupID,
						},
					},
				},
			},
		},
	}

	err := m.mcCtrlClient.Create(ctx, machineTemplate)
	if apierrors.IsAlreadyExists(err) {
		// It's ok. It's already there.
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (m *awsMigrator) createWorkersKubeadmConfigTemplate(ctx context.Context) error {
	// iterate over all nodepools (AWSMachineDeployments)
	for _, d := range m.crs.awsMachineDeployments {

		c := &bootstrap.KubeadmConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.AWSMachinePoolName(m.clusterID, d.Name),
				Namespace: d.Namespace,
			},
			Spec: bootstrap.KubeadmConfigSpec{
				PreKubeadmCommands: []string{
					"hostnamectl set-hostname $(curl http://169.254.169.254/latest/meta-data/local-hostname)",
				},
				InitConfiguration: &bootstraptypes.InitConfiguration{
					NodeRegistration: bootstraptypes.NodeRegistrationOptions{
						KubeletExtraArgs: map[string]string{
							"cloud-provider": "aws",
						},
						Name: "{{ ds.meta_data.local_hostname }}",
					},
				},
				JoinConfiguration: &bootstraptypes.JoinConfiguration{
					NodeRegistration: bootstraptypes.NodeRegistrationOptions{
						KubeletExtraArgs: map[string]string{
							"cloud-provider": "aws",
							"node-labels":    "node.kubernetes.io/worker,role=worker",
						},
						Name: "{{ ds.meta_data.local_hostname }}",
					},
				},
				Files: []bootstrap.File{
					{
						Path:  "/etc/kubernetes/config/kube-proxy.yaml",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.AWSCustomFilesSecretName(m.clusterID),
								Key:  "kubeProxyKubeconfigKey",
							},
						},
					},
					{
						Path:  "/etc/kubernetes/config/proxy-config.yml",
						Owner: "root:root",
						ContentFrom: &bootstrap.FileSource{
							Secret: bootstrap.SecretFileSource{
								Name: key.AWSCustomFilesSecretName(m.clusterID),
								Key:  kubeProxyConfigKey,
							},
						},
					},
				},
			},
		}

		err := m.mcCtrlClient.Create(ctx, c)
		if apierrors.IsAlreadyExists(err) {
			// It's ok. It's already there.
		} else if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func (m *awsMigrator) createWorkersAWSMachinePools(ctx context.Context) error {
	// iterate over all nodepools (AWSMachineDeployments)
	for _, d := range m.crs.awsMachineDeployments {
		// FETCH AWS INFO
		i := &ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("tag:Name"),
					Values: aws.StringSlice([]string{fmt.Sprintf("%s-worker", m.clusterID)}),
				},
				{
					Name:   aws.String("tag:giantswarm.io/machine-deployment"),
					Values: aws.StringSlice([]string{d.Name}),
				},
			},
		}

		o, err := m.awsClients.ec2Client.DescribeSecurityGroups(i)
		if err != nil {
			return microerror.Mask(err)
		}
		if len(o.SecurityGroups) != 1 {
			return microerror.Maskf(nil, "expected 1 master security group but found %d", len(o.SecurityGroups))
		}

		i2 := &ec2.DescribeSubnetsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("tag:giantswarm.io/machine-deployment"),
					Values: aws.StringSlice([]string{d.Name}),
				},
			},
		}

		o2, err := m.awsClients.ec2Client.DescribeSubnets(i2)
		if err != nil {
			return microerror.Mask(err)
		}

		// Create the CR
		awsmp := &capaexp.AWSMachinePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.AWSMachinePoolName(m.clusterID, d.Name),
				Namespace: d.Namespace,
			},
			Spec: capaexp.AWSMachinePoolSpec{
				MinSize: int32(d.Spec.NodePool.Scaling.Min),
				MaxSize: int32(d.Spec.NodePool.Scaling.Max),
				AWSLaunchTemplate: capaexp.AWSLaunchTemplate{
					Name:               d.Name,
					InstanceType:       d.Spec.Provider.Worker.InstanceType,
					SSHKeyName:         aws.String("vaclav"),
					IamInstanceProfile: "nodes.cluster-api-provider-aws.sigs.k8s.io",
					AdditionalSecurityGroups: []capa.AWSResourceReference{
						{
							ID: o.SecurityGroups[0].GroupId,
						},
					},
				},
			},
		}

		for _, subnet := range o2.Subnets {
			awsmp.Spec.Subnets = append(awsmp.Spec.Subnets, capa.AWSResourceReference{ID: subnet.SubnetId})
			awsmp.Spec.AvailabilityZones = append(awsmp.Spec.AvailabilityZones, *subnet.AvailabilityZone)
		}

		err = m.mcCtrlClient.Create(ctx, awsmp)
		if apierrors.IsAlreadyExists(err) {
			// It's ok. It's already there.
		} else if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func (m *awsMigrator) createWorkersMachinePools(ctx context.Context) error {
	k8sVersion := getReleaseComponents(m.crs.release)["K8sVersion"]

	for _, d := range m.crs.awsMachineDeployments {
		mp := &capiexp.MachinePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.AWSMachinePoolName(m.clusterID, d.Name),
				Namespace: d.Namespace,
			},
			Spec: capiexp.MachinePoolSpec{
				ClusterName: m.clusterID,
				Replicas:    aws.Int32(int32(d.Spec.NodePool.Scaling.Min)),
				Template: capi.MachineTemplateSpec{
					Spec: capi.MachineSpec{
						ClusterName: m.clusterID,
						Version:     &k8sVersion,
						InfrastructureRef: corev1.ObjectReference{
							Name:       key.AWSMachinePoolName(m.clusterID, d.Name),
							Namespace:  d.Namespace,
							Kind:       "AWSMachinePool",
							APIVersion: capiexp.GroupVersion.String(),
						},
						Bootstrap: capi.Bootstrap{
							ConfigRef: &corev1.ObjectReference{
								Name:       key.AWSMachinePoolName(m.clusterID, d.Name),
								Namespace:  d.Namespace,
								Kind:       "KubeadmConfig",
								APIVersion: bootstrap.GroupVersion.String(),
							},
						},
					},
				},
			},
		}

		err := m.mcCtrlClient.Create(ctx, mp)
		if apierrors.IsAlreadyExists(err) {
			// It's ok. It's already there.
		} else if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func (m *awsMigrator) readEncryptionSecret(ctx context.Context) error {
	obj := &corev1.Secret{}
	key := ctrl.ObjectKey{Namespace: "default", Name: key.EncryptionConfigSecretName(m.clusterID)}
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

func (m *awsMigrator) readAWSControlPlane(ctx context.Context) error {
	objList := &giantswarmawsalpha3.AWSControlPlaneList{}
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
	m.crs.awsControlPlane = &obj

	return nil
}

func (m *awsMigrator) readG8sControlPlane(ctx context.Context) error {
	objList := &giantswarmawsalpha3.G8sControlPlaneList{}
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
	m.crs.g8sControlPlane = &obj

	return nil
}

func (m *awsMigrator) readAWSMachineDeployments(ctx context.Context) error {
	objList := &giantswarmawsalpha3.AWSMachineDeploymentList{}
	selector := ctrl.MatchingLabels{capi.ClusterLabelName: m.clusterID}
	err := m.mcCtrlClient.List(ctx, objList, selector)
	if err != nil {
		return microerror.Mask(err)
	}

	m.crs.awsMachineDeployments = objList.Items

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
