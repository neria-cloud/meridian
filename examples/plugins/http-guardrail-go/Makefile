.PHONY: build run test clean docker

# A standalone module: built outside the repository's go.work, as a user builds it.
export GOWORK = off

PLUGIN_NAME = http-guardrail-go
OUTPUT_DIR  = build
OUTPUT      = $(OUTPUT_DIR)/$(PLUGIN_NAME)
ADDR       ?= 127.0.0.1:18082
MODE       ?= block

build: ## Build the plugin binary
	@mkdir -p $(OUTPUT_DIR)
	go build -trimpath -ldflags="-s -w" -o $(OUTPUT) .

run: build ## Build and start (ADDR=host:port MODE=block|mask)
	$(OUTPUT) -addr $(ADDR) -mode $(MODE)

test: ## Run tests
	go test ./...

docker: ## Build the container image
	docker build -t meridian-http-guardrail:latest .

clean:
	rm -rf $(OUTPUT_DIR)

.DEFAULT_GOAL := build
