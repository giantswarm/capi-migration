module github.com/giantswarm/capi-migration

go 1.16

require (
	github.com/giantswarm/certs/v3 v3.0.0
	github.com/giantswarm/k8sclient/v4 v4.0.0
	github.com/giantswarm/microerror v0.3.0
	github.com/giantswarm/micrologger v0.5.0
	github.com/giantswarm/tenantcluster/v3 v3.0.0
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.1
	k8s.io/api v0.18.9
	k8s.io/apimachinery v0.18.9
	k8s.io/client-go v0.18.9
	sigs.k8s.io/cluster-api v0.3.7
	sigs.k8s.io/controller-runtime v0.6.3
)

replace sigs.k8s.io/cluster-api => github.com/giantswarm/cluster-api v0.3.13-gs
