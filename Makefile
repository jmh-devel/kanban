APP := kanban
PKG := ./cmd/kanban
BUILD_DIR := bin
DEB_DIR := dist/deb
DEB_BASE_VERSION ?= 0.1.0
DEB_BUILD_TIMESTAMP := $(shell date -u +%Y%m%d%H%M%S)
DEB_BUILD_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo nogit)
DEB_VERSION ?= $(DEB_BASE_VERSION)+$(DEB_BUILD_TIMESTAMP).$(DEB_BUILD_COMMIT)
APP_VERSION ?= $(DEB_BASE_VERSION)
BUILD_TIMESTAMP ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_COMMIT ?= $(DEB_BUILD_COMMIT)
BUILD_DIRTY ?= $(shell test -z "$$(git status --porcelain 2>/dev/null)" && echo false || echo true)
GO_LDFLAGS := -s -w -X main.version=$(APP_VERSION) -X main.commit=$(BUILD_COMMIT) -X main.buildDate=$(BUILD_TIMESTAMP) -X main.dirty=$(BUILD_DIRTY)

.PHONY: fmt fmt-check vet test build run check clean deb install

fmt:
	gofmt -w ./cmd ./internal

fmt-check:
	@test -z "$(shell gofmt -l ./cmd ./internal)" || (echo "Run 'make fmt'" && gofmt -l ./cmd ./internal && exit 1)

vet:
	go vet ./...

test:
	go test ./...

build:
	mkdir -p $(BUILD_DIR)
	go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BUILD_DIR)/$(APP) $(PKG)

deb: build
	rm -rf $(DEB_DIR)
	mkdir -p $(DEB_DIR)/$(APP)_$(DEB_VERSION)/DEBIAN
	mkdir -p $(DEB_DIR)/$(APP)_$(DEB_VERSION)/usr/bin
	mkdir -p $(DEB_DIR)/$(APP)_$(DEB_VERSION)/etc/default
	mkdir -p $(DEB_DIR)/$(APP)_$(DEB_VERSION)/lib/systemd/system
	cp packaging/debian/control $(DEB_DIR)/$(APP)_$(DEB_VERSION)/DEBIAN/control
	sed -i "s/^Version:.*/Version: $(DEB_VERSION)/" $(DEB_DIR)/$(APP)_$(DEB_VERSION)/DEBIAN/control
	cp $(BUILD_DIR)/$(APP) $(DEB_DIR)/$(APP)_$(DEB_VERSION)/usr/bin/$(APP)
	cp packaging/systemd/kanban.default $(DEB_DIR)/$(APP)_$(DEB_VERSION)/etc/default/$(APP)
	cp packaging/systemd/kanban.service $(DEB_DIR)/$(APP)_$(DEB_VERSION)/lib/systemd/system/$(APP).service
	dpkg-deb --root-owner-group --build $(DEB_DIR)/$(APP)_$(DEB_VERSION)
	@echo "Built: $(DEB_DIR)/$(APP)_$(DEB_VERSION).deb"

install: deb
	sudo dpkg -i $(DEB_DIR)/$(APP)_$(DEB_VERSION).deb

run:
	go run $(PKG) serve

check: fmt-check vet test

clean:
	rm -rf $(BUILD_DIR) dist coverage.out
