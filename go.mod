module github.com/giantswarm/capi-migration

go 1.16

require (
	github.com/giantswarm/apiextensions/v3 v3.22.0
	github.com/giantswarm/microerror v0.3.0
	github.com/giantswarm/micrologger v0.5.0
	github.com/onsi/ginkgo v1.14.2
	github.com/onsi/gomega v1.10.3
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.20.2
	sigs.k8s.io/cluster-api v0.3.14
	sigs.k8s.io/cluster-api-provider-azure v0.4.13
	sigs.k8s.io/controller-runtime v0.8.2
)

replace (
	sigs.k8s.io/cluster-api => github.com/giantswarm/cluster-api v0.3.13-gs
	sigs.k8s.io/cluster-api-provider-azure => github.com/giantswarm/cluster-api-provider-azure v0.4.12-gsalpha3
)
