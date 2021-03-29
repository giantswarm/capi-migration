
# Image URL to use all building/pushing image targets
IMG ?= giantswarm/capi-migration:latest
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Build manager binary
manager: generate fmt vet
	go build -o bin/manager main.go

run:
	@echo "Deprecated: use \"make run-aws\" or \"make run-azure\"" >&2 && exit 1

# Run against the configured Kubernetes cluster in ~/.kube/config
run-aws: generate fmt vet manifests
	go run ./main.go --provider=aws

# Run against the configured Kubernetes cluster in ~/.kube/config
run-azure: generate fmt vet manifests
	go run ./main.go --provider=azure

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: NAME_SUFFIX ?= $(USER)
deploy:
	cd config/dev && kustomize edit set image controller=$(IMG)
	cd config/dev && kustomize edit set namesuffix -- -$(NAME_SUFFIX)
	kustomize build config/dev | kubectl apply -f -

# Undeploy controller in the configured Kubernetes cluster in ~/.kube/config
undeploy: NAME_SUFFIX ?= $(USER)
undeploy: manifests
	cd config/dev && kustomize edit set namesuffix -- -$(NAME_SUFFIX)
	kustomize build config/dev | kubectl delete -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: CHART_TEMPLATE_FILE := $(shell ls -d helm/$$(basename $$(go list -m)))/templates/kustomize-out.yaml
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	mkdir -p $(shell dirname $(CHART_TEMPLATE_FILE))
	kustomize build config/helm -o '$(CHART_TEMPLATE_FILE)'

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Build the docker image
docker-build: test
	mkdir -p api controllers
	docker build . -t ${IMG}

# Push the docker image
docker-push:
	docker push ${IMG}

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.5 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif
