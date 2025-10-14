SHELL := /bin/bash

CACHE_DIR := .gocache
TMP_DIR := .gotmp
MODCACHE_DIR := .gomodcache
BIN_DIR := bin

PROJECT_ID ?=
REGION ?= us-central1
SERVICE ?= pdf2jpg
TAG ?= latest
LOCAL_IMAGE ?= pdf2jpg:local
REMOTE_IMAGE ?= us-central1-docker.pkg.dev/$(PROJECT_ID)/pdf2jpg/pdf2jpg:$(TAG)

GO_ENV := GOCACHE=$(CURDIR)/$(CACHE_DIR) GOTMPDIR=$(CURDIR)/$(TMP_DIR) GOMODCACHE=$(CURDIR)/$(MODCACHE_DIR)

.PHONY: prepare-cache tidy build install test unit e2e clean docker-build docker-push deploy

prepare-cache:
	@mkdir -p $(CACHE_DIR) $(TMP_DIR) $(MODCACHE_DIR) $(BIN_DIR)

tidy: prepare-cache
	@$(GO_ENV) go mod tidy

build: tidy
	@$(GO_ENV) go build ./...

install: build
	@$(GO_ENV) GOBIN=$(CURDIR)/$(BIN_DIR) go install ./cmd
	@if [ -f $(BIN_DIR)/cmd ]; then mv $(BIN_DIR)/cmd $(BIN_DIR)/main; fi

test: install
	@$(GO_ENV) go test ./...

unit: install
	@$(GO_ENV) go test ./internal/...

e2e: install
	@$(GO_ENV) go test ./test -run TestConvertEndpoint

clean:
	@rm -rf $(CACHE_DIR) $(TMP_DIR) $(MODCACHE_DIR) $(BIN_DIR)

docker-build:
	@DOCKER_BUILDKIT=1 docker build --file Dockerfile --tag $(LOCAL_IMAGE) .

docker-push: docker-build
	@if [ -z "$(PROJECT_ID)" ]; then echo "PROJECT_ID must be set for docker-push"; exit 1; fi
	@docker tag $(LOCAL_IMAGE) $(REMOTE_IMAGE)
	@docker push $(REMOTE_IMAGE)

deploy: docker-push
	@gcloud run deploy $(SERVICE) \
		--project $(PROJECT_ID) \
		--region $(REGION) \
		--image $(REMOTE_IMAGE) \
		--allow-unauthenticated \
		--set-env-vars PORT=8080 \
		--set-secrets API_KEYS=projects/$(PROJECT_ID)/secrets/pdf2jpg-api-key:latest
