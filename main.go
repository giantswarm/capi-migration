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
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	vaultapi "github.com/hashicorp/vault/api"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	capiv1alpha3 "sigs.k8s.io/cluster-api/api/v1alpha3"
	// +kubebuilder:scaffold:imports

	"github.com/giantswarm/capi-migration/controllers"
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
	enableLeaderElection bool
	metricsAddr          string
	vaultAddr            string
	vaultToken           string
}{}

func initFlags() {
	flag.BoolVar(&flags.enableLeaderElection, "enable-leader-election", false, "Enable leader election for controller manager.")
	flag.StringVar(&flags.vaultAddr, "vault-addr", os.Getenv("VAULT_ADDR"), "The address of the vault to connect to. Defaults to VAULT_ADDR.")
	flag.StringVar(&flags.vaultToken, "vault-token", os.Getenv("VAULT_TOKEN"), "The token to use to authenticate to vault. Defaults to VAULT_TOKEN.")
	flag.StringVar(&flags.metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.Parse()

	var errors []string

	if flags.vaultAddr == "" {
		errors = append(errors, "--vault-addr flag or VAULT_ADDR environment variable must be set")
	}
	if flags.vaultToken == "" {
		errors = append(errors, "--vault-token flag or VAULT_TOKEN environment variable must be set")
	}

	if len(errors) > 1 {
		fmt.Fprintf(os.Stderr, "%s\n", strings.Join(errors, "\n"))
		os.Exit(2)
	}
}

func main() {
	initFlags()
	err := mainE(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", microerror.Pretty(err, true))
		os.Exit(2)
	}
}

type VaultClient struct {
	client *vaultapi.Client
}

func NewVaultClient(client *vaultapi.Client) *VaultClient {
	return &VaultClient{
		client: client,
	}
}

func (v *VaultClient) Request(ctx context.Context, method, endpoint string, req, resp interface{}) error {
	httpReq := v.client.NewRequest(method, endpoint)
	err := httpReq.SetJSONBody(req)
	if err != nil {
		return microerror.Mask(err)
	}

	httpResp, err := v.client.RawRequest(httpReq)
	if err != nil {
		return microerror.Mask(err)
	}

	if httpResp.StatusCode != 200 {
		return microerror.Mask(fmt.Errorf("expected status code = 200, got %d", httpResp.StatusCode))
	}

	err = httpResp.DecodeJSON(resp)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func mainE(ctx context.Context) error {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: flags.metricsAddr,
		Port:               9443,
		LeaderElection:     flags.enableLeaderElection,
		LeaderElectionID:   "56d80c47.giantswarm.io",
	})
	if err != nil {
		return microerror.Mask(err)
	}

	log, err := micrologger.New(micrologger.Config{})
	if err != nil {
		return microerror.Mask(err)
	}

	var vaultClient *vaultapi.Client
	{
		c := vaultapi.DefaultConfig()
		c.Address = flags.vaultAddr
		vaultClient, err = vaultapi.NewClient(c)
		if err != nil {
			return nil
		}
		vaultClient.SetToken(flags.vaultToken)

		// Check vault connectivity.
		_, err := vaultClient.Auth().Token().LookupSelf()
		if err != nil {
			return microerror.Mask(err)
		}
	}

	if err = (&controllers.ClusterReconciler{
		Client:      mgr.GetClient(),
		Log:         log,
		VaultClient: vaultClient,
		Scheme:      mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		return microerror.Mask(err)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return microerror.Mask(err)
	}

	return nil
}
