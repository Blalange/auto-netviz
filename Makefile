.PHONY: all build build-server build-collector build-web test vet lint-helm clean dev-server dev-collector-demo dev-web

BINARY_DIR ?= bin

all: build

build: build-server build-collector build-web

build-server:
	mkdir -p $(BINARY_DIR)
	go build -o $(BINARY_DIR)/auto-netviz-server ./cmd/server

build-collector:
	mkdir -p $(BINARY_DIR)
	go build -o $(BINARY_DIR)/auto-netviz-collector ./cmd/collector

build-web:
	cd web && npm run build

test:
	go test -race ./...

vet:
	go vet ./...

lint-helm:
	helm lint charts/auto-netviz

dev-server:
	go run ./cmd/server --manual ./examples/manual --db ./data/dev.db --listen :8080

dev-collector-demo:
	go run ./cmd/collector --demo --server-ws ws://127.0.0.1:8080/ws/collector

dev-web:
	cd web && npm run dev

clean:
	rm -rf $(BINARY_DIR) data spool web/build web/.svelte-kit
