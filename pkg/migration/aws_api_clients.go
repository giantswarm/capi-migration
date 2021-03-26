package migration

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/route53"
	giantswarmawsalpha3 "github.com/giantswarm/apiextensions/v3/pkg/apis/infrastructure/v1alpha2"
	"github.com/giantswarm/microerror"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

type AWSConfig struct {
	AccessKeyID     string
	AccessKeySecret string

	Region  string
	RoleARN string
}

type awsClients struct {
	asgClient     *autoscaling.AutoScaling
	ec2Client     *ec2.EC2
	route53Client *route53.Route53
}

// createAWSApiClients create all necessary aws api clients fro later use
func (m *awsMigrator) createAWSApiClients(ctx context.Context) error {
	arn, err := m.getClusterCredentialARN(ctx, m.crs.awsCluster)
	if err != nil {
		return microerror.Mask(err)
	}
	m.awsCredentials.RoleARN = arn
	m.awsCredentials.Region = m.crs.awsCluster.Spec.Provider.Region

	awsClients, err := getAWSClients(m.awsCredentials)
	if err != nil {
		return microerror.Mask(err)
	}

	m.awsClients = awsClients
	return nil
}

// getClusterCredentialARN get cluster credential ARN to assume role in specific AWS WC account
func (m *awsMigrator) getClusterCredentialARN(ctx context.Context, cr *giantswarmawsalpha3.AWSCluster) (string, error) {
	secret := &corev1.Secret{}
	secretKey := ctrl.ObjectKey{Name: cr.Spec.Provider.CredentialSecret.Name, Namespace: cr.Spec.Provider.CredentialSecret.Namespace}

	err := m.mcCtrlClient.Get(ctx, secretKey, secret)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return string(secret.Data["aws.awsoperator.arn"]), nil
}

func getAWSClients(config AWSConfig) (*awsClients, error) {
	var err error
	var s *session.Session
	{
		c := &aws.Config{
			Credentials: credentials.NewStaticCredentials(config.AccessKeyID, config.AccessKeySecret, ""),
			Region:      aws.String(config.Region),
		}

		s, err = session.NewSession(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	credentialsConfig := &aws.Config{
		Credentials: stscreds.NewCredentials(s, config.RoleARN),
	}

	o := &awsClients{
		ec2Client:     ec2.New(s, credentialsConfig),
		route53Client: route53.New(s, credentialsConfig),
		asgClient:     autoscaling.New(s, credentialsConfig),
	}

	return o, nil
}
