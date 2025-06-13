GOBINARY=go
BINARY_NAME=dist/nomad-driver-tart

.PHONY: build
build:
	@echo "Building Nomad Tart driver plugin..."
	@$(GOBINARY) build -o $(BINARY_NAME) main.go
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
