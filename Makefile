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

e2e: docker-build
	kind create cluster --config ./e2e/config.yaml
	kubectl apply -f ./e2e/manifests.yaml
	kind load docker-image ${IMG}
	helm upgrade --install --create-namespace --namespace="node-ttl" node-ttl ./charts/node-ttl --set "image.pullPolicy=Never" --set "nodeTtl.interval=10s" --set "image.tag=${TAG}"
	CGO_ENABLED=0 go test ./e2e -cover -v
