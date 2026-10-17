GOBINARY=go
BINARY_NAME=dist/nomad-driver-tart
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || echo 0.0.0-dev)
LDFLAGS = -X github.com/brianmichel/nomad-driver-tart/driver.Version=$(patsubst v%,%,$(VERSION))

.PHONY: build
build:
	@echo "Building Nomad Tart driver plugin..."
	@$(GOBINARY) build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) main.go
	@echo "Build complete: $(BINARY_NAME)"

.PHONY: clean
clean:
	@echo "Cleaning up..."
	@rm -f $(BINARY_NAME)
	@echo "Cleanup complete."

.PHONY: fmt
fmt:
	@echo "Formatting code..."
	@$(GOBINARY) fmt ./...

.PHONY: vet
vet:
	@echo "Vetting code..."
	@$(GOBINARY) vet ./...

.PHONY: test
test:
	@echo "Running tests... (no tests yet)"
	# @$(GOBINARY) test ./... -v

.PHONY: all
all: fmt vet build

# Default target
.DEFAULT_GOAL := all
