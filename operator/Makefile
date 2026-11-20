export PATH := /usr/local/go/bin:$(PATH)

.PHONY: tidy build install-crds

tidy:
	go mod tidy

build:
	go build -o bin/kubeops-console ./cmd/console
	go build -o bin/kubeops-authentication ./cmd/oidc

install-crds:
	kubectl apply -f config/crd/
