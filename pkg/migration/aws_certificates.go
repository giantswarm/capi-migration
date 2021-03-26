package migration

import (
	"context"

	"github.com/giantswarm/microerror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/giantswarm/capi-migration/pkg/migration/internal/key"
)

// migrateCertsSecrets do necessary changes to certs to be compatible with CAPI
func (m *awsMigrator) migrateCertsSecrets(ctx context.Context) error {
	err := m.createCASecrets(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = m.moveCertDataToCAPIKeys(ctx, key.SACertsSecretName(m.clusterID))
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// moveCertDataToCAPIKeys adjust 'keys' in secrets containt certificate to match CAPI expectation
// ie: certificate data are moved from key 'cert' to 'tls.crt', key data are moved from 'key' to 'tls.key'
func (m *awsMigrator) moveCertDataToCAPIKeys(ctx context.Context, secretName string) error {
	secret := &corev1.Secret{}
	secretKey := ctrl.ObjectKey{Namespace: "default", Name: secretName}
	err := m.mcCtrlClient.Get(ctx, secretKey, secret)
	if err != nil {
		return microerror.Mask(err)
	}
	secret.Data["tls.crt"] = secret.Data["cert"]
	secret.Data["tls.key"] = secret.Data["key"]

	err = m.mcCtrlClient.Update(ctx, secret)
	if err != nil {
		return microerror.Mask(err)
	}
	return nil
}

// createCASecrets fetches CA private key from vault and save it to 'clusterID-ca` and 'clusterID-etcd' secret into MC
func (m *awsMigrator) createCASecrets(ctx context.Context) error {
	caPrivKey, caCertData, err := m.getCAData(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"tls.crt": caCertData,
			"tls.key": caPrivKey,
		},
	}

	err = m.mcCtrlClient.Create(ctx, secret)
	if apierrors.IsAlreadyExists(err) {
		// ignore already exists error
	} else if err != nil {
		return microerror.Mask(err)
	}

	// FOR NOW, REPLACE DATA IN ETCD CERT AS WELL
	etcdSecret := &corev1.Secret{}
	etcdSecretKey := ctrl.ObjectKey{Namespace: "default", Name: key.EtcdCertsSecretName(m.clusterID)}
	err = m.mcCtrlClient.Get(ctx, etcdSecretKey, etcdSecret)
	if err != nil {
		return microerror.Mask(err)
	}
	etcdSecret.Data["tls.crt"] = caCertData
	etcdSecret.Data["tls.key"] = caPrivKey

	err = m.mcCtrlClient.Update(ctx, etcdSecret)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// getCAData reads valut PKI endpoint and fetches CA private key and CA certificate
func (m *awsMigrator) getCAData(ctx context.Context) ([]byte, []byte, error) {
	// Will be implemented once vault client is avaiable

	keyData := []byte("TODO")
	certData := []byte("TODO")

	return keyData, certData, nil
}
