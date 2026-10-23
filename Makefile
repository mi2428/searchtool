COMPOSE := docker compose
.PHONY: check fmt build image dev down

check:
	go mod tidy -diff
	golangci-lint fmt --diff
	go vet ./...
	go test -race -count=1 ./...
	golangci-lint run ./...
	hadolint Dockerfile
	yamllint -d '{extends: relaxed, rules: {line-length: disable}}' docker-compose.yml k8s/base
	$(COMPOSE) --env-file .env.example config --quiet
	bash -o pipefail -c 'kubectl kustomize k8s/base | kubeconform -strict -summary'

fmt:
	golangci-lint fmt

build:
	mkdir -p build
	go build -trimpath -o build/searchtool ./cmd/searchtool

image:
	$(COMPOSE) --env-file .env.example build searchtool

dev:
	$(COMPOSE) up --build searchtool

down:
	$(COMPOSE) down
