COMPOSE := docker compose
.DEFAULT_GOAL := help
.PHONY: help check build image dev down fmt

##@ Quality

check: ## Run Go tests, static checks, container and Kubernetes validation
	go mod tidy -diff
	golangci-lint fmt --diff
	go vet ./...
	go test -race -count=1 ./...
	golangci-lint run ./...
	hadolint Dockerfile
	yamllint -d '{extends: relaxed, rules: {line-length: disable}}' docker-compose.yml k8s/base
	$(COMPOSE) --env-file .env.example config --quiet
	bash -o pipefail -c 'kubectl kustomize k8s/base | kubeconform -strict -summary'
fmt: ## Format Go source
	golangci-lint fmt

##@ Development

build: ## Build the Search Tool
	mkdir -p build
	go build -trimpath -o build/searchtool ./cmd/searchtool

image: ## Build amd64/arm64 images locally (no push)
	docker buildx build --platform linux/amd64,linux/arm64 --tag ghcr.io/mi2428/searchtool:latest --load .

dev: ## Start the Search Tool (requires .env)
	$(COMPOSE) up --build searchtool

down: ## Stop local Compose services
	$(COMPOSE) down

##@ Help

help: ## Show available commands
	@awk 'BEGIN {FS = ":.*##"; section = ""} \
	/^[a-zA-Z0-9_.-]+:.*##/ { \
		if (section != "") printf "\n\033[1m%s\033[0m\n", section; \
		section = ""; \
		printf "  \033[36m%-11s\033[0m %s\n", $$1, $$2; next \
	} \
	/^##@/ { section = substr($$0, 5); next }' $(MAKEFILE_LIST)
