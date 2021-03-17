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

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/giantswarm/capi-migration/controllers"
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
	EnableLeaderElection bool
	MetricsAddr          string
}{}

func initFlags() {
	flag.BoolVar(&flags.EnableLeaderElection, "enable-leader-election", false, "Enable leader election for controller manager.")
	flag.StringVar(&flags.MetricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.Parse()

	var errors []string

	// Flag validation goes here.
	//
	//if flags.MyFlag == "" {
	//	errors = append(errors, "--my-flag must be not empty")
	//}

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
		MetricsBindAddress: flags.MetricsAddr,
		Port:               9443,
		LeaderElection:     flags.EnableLeaderElection,
		LeaderElectionID:   "2db8ae24.giantswarm.io",
	})
	if err != nil {
		return microerror.Mask(err)
	}

	if err = (&controllers.ClusterReconciler{
		Client: mgr.GetClient(),
		Log:    log,
		Scheme: mgr.GetScheme(),
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
