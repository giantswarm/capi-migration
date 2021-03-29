/*
Copyright 2021 Giant Swarm.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	providerv1alpha1 "github.com/giantswarm/apiextensions/v3/pkg/apis/provider/v1alpha1"
	releasev1alpha1 "github.com/giantswarm/apiextensions/v3/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/certs/v3/pkg/certs"
	"github.com/giantswarm/k8sclient/v4/pkg/k8sclient"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/giantswarm/tenantcluster/v3/pkg/tenantcluster"
	vaultapi "github.com/hashicorp/vault/api"
	flag "github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	capzv1alpha3 "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha3"
	expcapzv1alpha3 "sigs.k8s.io/cluster-api-provider-azure/exp/api/v1alpha3"
	capiv1alpha3 "sigs.k8s.io/cluster-api/api/v1alpha3"
	bootstrapkubeadmv1alpha3 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha3"
	controlplanekubeadmv1alpha3 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	expcapiv1alpha3 "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// +kubebuilder:scaffold:imports

	"github.com/giantswarm/capi-migration/controllers"
	"github.com/giantswarm/capi-migration/pkg/migration"
	"github.com/giantswarm/capi-migration/pkg/project"
)

const (
	providerAWS   = "aws"
	providerAzure = "azure"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = capiv1alpha3.AddToScheme(scheme)
	_ = providerv1alpha1.AddToScheme(scheme)
	_ = capzv1alpha3.AddToScheme(scheme)
	_ = expcapiv1alpha3.AddToScheme(scheme)
	_ = expcapzv1alpha3.AddToScheme(scheme)
	_ = releasev1alpha1.AddToScheme(scheme)
	_ = bootstrapkubeadmv1alpha3.AddToScheme(scheme)
	_ = controlplanekubeadmv1alpha3.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

var flags = struct {
	AWSAccessKeyID     string
	AWSAccessKeySecret string
	LeaderElect        bool
	MetricsBindAddress string
	Provider           string
	VaultAddr          string
	VaultToken         string
}{}

func initFlags() (errors []error) {
	var configPaths []string
	flag.StringArrayVar(&configPaths, "config", []string{}, "List of paths to configuration file in yaml format with flat, kebab-case keys.")

	// Flag/configuration names.
	const (
		flagAWSAccessKeyID     = "aws-access-id"
		flagAWSAccessKeySecret = "aws-access-secret" //lint:nosec
		flagLeaderElect        = "leader-elect"
		flagMetricsBindAddres  = "metrics-bind-address"
		flagProvider           = "provider"
		flagVaultAddr          = "vault-addr"
		flagVaultToken         = "vault-token"
	)

	// Flag binding.
	flag.StringVar(&flags.AWSAccessKeyID, flagAWSAccessKeyID, "", "AWS access key for MC.")
	flag.StringVar(&flags.AWSAccessKeySecret, flagAWSAccessKeySecret, "", "AWS secret key for MC.")
	flag.BoolVar(&flags.LeaderElect, flagLeaderElect, false, "Enable leader election for controller manager.")
	flag.StringVar(&flags.MetricsBindAddress, flagMetricsBindAddres, ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&flags.Provider, flagProvider, "", "Provider name for the migration.")
	flag.String(flagVaultAddr, "", "The address of the vault to connect to. Defaults to VAULT_ADDR.")
	flag.String(flagVaultToken, "", "The token to use to authenticate to vault. Defaults to VAULT_TOKEN.")

	// Parse flags and configuration.
	flag.Parse()
	errors = append(errors, initFlagsFromEnv()...)

	// Extra env bindings.

	if flags.VaultAddr == "" {
		flags.VaultAddr = os.Getenv("VAULT_ADDR")
	}
	if flags.VaultToken == "" {
		flags.VaultToken = os.Getenv("VAULT_ADDR")
	}

	// Validation.

	if flags.Provider != providerAWS && flags.Provider != providerAzure {
		errors = append(errors, fmt.Errorf("--%s must be either \"%s\" or \"%s\"", flagProvider, providerAWS, providerAzure))
	}
	if flags.Provider == providerAWS && (flags.AWSAccessKeyID == "" || flags.AWSAccessKeySecret == "") {
		errors = append(errors, fmt.Errorf("when \"aws\" provider is set, --%s and --%s must not be empty", flagAWSAccessKeyID, flagAWSAccessKeySecret))
	}
	if flags.VaultAddr == "" {
		errors = append(errors, fmt.Errorf("--%s flag or VAULT_ADDR environment variable must be set", flagVaultAddr))
	}
	if flags.VaultToken == "" {
		errors = append(errors, fmt.Errorf("--%s flag or VAULT_TOKEN environment variable must be set", flagVaultToken))
	}

	return
}

func initFlagsFromEnv() (errors []error) {
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		if f.Changed {
			return
		}
		env := project.Name() + "_" + f.Name
		env = strings.ReplaceAll(env, ".", "_")
		env = strings.ReplaceAll(env, "-", "_")
		env = strings.ToUpper(env)
		v, ok := os.LookupEnv(env)
		if !ok {
			return
		}
		err := f.Value.Set(v)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to set --%s value using %q environment variable", f.Name, env))
		}
	})

	return
}

func main() {
	errs := initFlags()
	if len(errs) > 0 {
		ss := make([]string, len(errs))
		for i := range errs {
			ss[i] = errs[i].Error()
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", strings.Join(ss, "\nError: "))
		os.Exit(2)
	}

	err := mainE(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", microerror.Pretty(err, true))
		os.Exit(1)
	}
}

func mainE(ctx context.Context) error {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	log, err := micrologger.New(micrologger.Config{})
	if err != nil {
		return microerror.Mask(err)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: flags.MetricsBindAddress,
		Port:               9443,
		LeaderElection:     flags.LeaderElect,
		LeaderElectionID:   "2db8ae24.giantswarm.io",
	})
	if err != nil {
		return microerror.Mask(err)
	}

	var vaultClient *vaultapi.Client
	{
		c := vaultapi.DefaultConfig()
		c.Address = flags.VaultAddr
		vaultClient, err = vaultapi.NewClient(c)
		if err != nil {
			return nil
		}
		vaultClient.SetToken(flags.VaultToken)

		// Check vault connectivity.
		_, err := vaultClient.Auth().Token().LookupSelf()
		if err != nil {
			return microerror.Mask(err)
		}
	}
	var certsSearcher *certs.Searcher
	{
		clients, err := k8sclient.NewClients(k8sclient.ClientsConfig{
			Logger:     log,
			RestConfig: mgr.GetConfig(),
		})
		if err != nil {
			return microerror.Mask(err)
		}

		c := certs.Config{
			K8sClient: clients.K8sClient(),
			Logger:    log,

			WatchTimeout: 30 * time.Second,
		}

		certsSearcher, err = certs.NewSearcher(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var tenantCluster *tenantcluster.TenantCluster
	{
		tenantCluster, err = tenantcluster.New(tenantcluster.Config{
			CertsSearcher: certsSearcher,
			Logger:        log,
			CertID:        certs.APICert,
		})
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var migratorFactory migration.MigratorFactory
	{
		switch flags.Provider {
		case providerAWS:
			migratorFactory, err = migration.NewAWSMigratorFactory(migration.AWSMigrationConfig{
				AWSCredentials: migration.AWSConfig{
					AccessKeyID:     flags.AWSAccessKeyID,
					AccessKeySecret: flags.AWSAccessKeySecret,
				},
				CtrlClient:    mgr.GetClient(),
				Logger:        log,
				TenantCluster: tenantCluster,
			})

			if err != nil {
				return microerror.Mask(err)
			}
		case providerAzure:
			migratorFactory, err = migration.NewAzureMigratorFactory(migration.AzureMigrationConfig{
				CtrlClient:    mgr.GetClient(),
				Logger:        log,
				TenantCluster: tenantCluster,
			})
			if err != nil {
				return microerror.Mask(err)
			}
		default:
			return microerror.Mask(fmt.Errorf("unknown provider %#q", flags.Provider))
		}

	}

	if err = (&controllers.ClusterReconciler{
		Client:          mgr.GetClient(),
		Log:             log,
		MigratorFactory: migratorFactory,
		VaultClient:     vaultClient,
		Scheme:          mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		return microerror.Mask(err)
	}
	// +kubebuilder:scaffold:builder

	log.Debugf(ctx, "Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func must(err error) {
	if err != nil {
		panic(microerror.Pretty(err, true))
	}
}
