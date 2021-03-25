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

	"github.com/giantswarm/certs/v3/pkg/certs"
	"github.com/giantswarm/tenantcluster/v3/pkg/tenantcluster"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/giantswarm/capi-migration/controllers"
	"github.com/giantswarm/capi-migration/pkg/migration"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	capiv1alpha3 "sigs.k8s.io/cluster-api/api/v1alpha3"
	// +kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = capiv1alpha3.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

var flags = struct {
	LeaderElect        bool
	MetricsBindAddress string
}{}

func initFlags() (errors []error) {
	var configPaths []string
	flag.StringArrayVar(&configPaths, "config", []string{}, "List of paths to configuration file in yaml format with flat, kebab-case keys.")

	// Flag/configuration names.
	const (
		flagLeaderElect       = "leader-elect"
		flagMetricsBindAddres = "metrics-bind-address"
	)

	// Flag binding.
	flag.Bool(flagLeaderElect, false, "Enable leader election for controller manager.")
	flag.String(flagMetricsBindAddres, ":8080", "The address the metric endpoint binds to.")

	// Parse flags and configuration.
	flag.Parse()
	if err := initViper(configPaths); err != nil {
		errors = append(errors, fmt.Errorf("failed to read configuration with error: %s", err))
		return
	}

	// Value binding.
	flags.LeaderElect = viper.GetBool(flagLeaderElect)
	flags.MetricsBindAddress = viper.GetString(flagMetricsBindAddres)

	// Validation.

	//if flags.MyFlag == "" {
	//	errors = append(errors, fmt.Errorf("--%s must be not empty", flagMyFlag))
	//}
	return
}

func initViper(configPaths []string) (errors []error) {
	viper.BindPFlags(flag.CommandLine)
	if len(configPaths) > 0 {
		for _, p := range configPaths {
			viper.AddConfigPath(p)
		}
	}
	err := viper.ReadInConfig()
	if err != nil {
		errors = append(errors, err)
		return
	}
	return
}

func main() {
	errs := initFlags()
	if len(errs) > 0 {
		ss := make([]string, len(errs))
		for i := range errs {
			ss[i] = errs[i].Error()
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", strings.Join(ss, "\n Error:"))
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

	var certsSearcher *certs.Searcher
	{
		client, err := kubernetes.NewForConfig(mgr.GetConfig())
		if err != nil {
			return microerror.Mask(err)
		}

		c := certs.Config{
			K8sClient: client,
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

	var azureMigratorFactory migration.MigratorFactory
	{
		azureMigratorFactory, err = migration.NewAzureMigratorFactory(migration.AzureMigrationConfig{
			Logger:        log,
			TenantCluster: tenantCluster,
		})
		if err != nil {
			return microerror.Mask(err)
		}
	}

	if err = (&controllers.ClusterReconciler{
		Client:          mgr.GetClient(),
		Log:             log,
		MigratorFactory: azureMigratorFactory,
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
