TAG = $$(git rev-parse --short HEAD)
IMG ?= ghcr.io/xenitab/node-ttl:$(TAG)

all: fmt vet lint

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

test: fmt vet
	go test --cover ./...

docker-build:
	docker build -t ${IMG} .

.PHONY: e2e
.ONESHELL:
e2e: docker-build docker-build-cluster-autoscaler
	set -ex

	TMP_DIR=$$(mktemp -d)
	export KIND_KUBECONFIG=$$TMP_DIR/kind.kubeconfig
	echo $$KIND_KUBECONFIG
	export KUBEMARK_KUBECONFIG=$$TMP_DIR/kubemark.kubeconfig

	# Create kind cluster and load images
	kind create cluster --kubeconfig $$KIND_KUBECONFIG
	kind load docker-image ${IMG}
	kind load docker-image staging-k8s.gcr.io/cluster-autoscaler-amd64:dev
	docker pull quay.io/elmiko/kubemark:v1.25.3
	kind load docker-image quay.io/elmiko/kubemark:v1.25.3

	# Start hollow node
	kubectl --kubeconfig $$KIND_KUBECONFIG apply -f ./e2e/hollow-node.yaml
	cp $$KIND_KUBECONFIG $$KUBEMARK_KUBECONFIG
	kubectl config --kubeconfig $$KUBEMARK_KUBECONFIG set-cluster kind-kind --server=https://kubernetes.default
	kubectl --kubeconfig $$KIND_KUBECONFIG create secret generic kubeconfig --type=Opaque --namespace=kubemark --from-file=kubelet.kubeconfig=$$KUBEMARK_KUBECONFIG --from-file=kubeproxy.kubeconfig=$$KUBEMARK_KUBECONFIG
	kubectl --kubeconfig $$KIND_KUBECONFIG --namespace kubemark wait --timeout=300s --for=jsonpath="{.status.readyReplicas}"=1 replicationcontroller/hollow-node
	
	# Start cluster autoscaler
	kubectl --kubeconfig $$KIND_KUBECONFIG apply -f ./e2e/cluster-autoscaler.yaml
	
	# Start pause workloads
	kubectl --kubeconfig $$KIND_KUBECONFIG apply -f ./e2e/pause-workloads.yaml
	kubectl --kubeconfig $$KIND_KUBECONFIG --namespace default wait --timeout=300s --for=jsonpath="{.status.active}"=1 job/pause
	kubectl --kubeconfig $$KIND_KUBECONFIG --namespace default wait --timeout=300s --for=jsonpath="{.status.availableReplicas}"=3 deployment/pause
	kubectl --kubeconfig $$KIND_KUBECONFIG --namespace default wait --timeout=300s --for=jsonpath="{.status.availableReplicas}"=3 statefulset/pause

	# Start node ttl
	helm upgrade --kubeconfig $$KIND_KUBECONFIG --install --create-namespace --namespace="node-ttl" node-ttl ./charts/node-ttl --set "image.pullPolicy=Never" --set "nodeTtl.interval=10s" --set "image.tag=${TAG}"

	# Run tests
	go test ./e2e/e2e_test.go -cover -v -timeout 300s

	# Delete cluster
	kind delete cluster

docker-build-cluster-autoscaler:
	TMP_DIR=$$(mktemp -d)
	cd $$TMP_DIR
	git clone https://github.com/kubernetes/autoscaler
	cd autoscaler/cluster-autoscaler
	git checkout cluster-autoscaler-1.25.0
	BUILD_TAGS=kubemark make build
	make make-image
