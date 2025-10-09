IMG_NAME ?= cdi-dra
IMG_TAG ?= latest
IMG ?= $(IMG_NAME):$(IMG_TAG)

# Directories
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Tooling
GO ?= go

# --------------------------
# Build Targets
# --------------------------
.PHONY: all
all: build

.PHONY: build
build:
	$(GO) build -o $(LOCALBIN)/composable-dra-driver main.go

.PHONY: run
run:
	$(GO) run main.go

# --------------------------
# Testing Targets
# --------------------------
.PHONY: test
test:
	$(GO) test ./...

# --------------------------
# Image Management
# --------------------------
.PHONY: docker-build
docker-build:
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push:
	docker push $(IMG)