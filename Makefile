BINARY := mongoquery
PREFIX ?= $(HOME)/.local
INSTALL_DIR := $(PREFIX)/bin
CONFIG := $(if $(MONGOQUERY_CONFIG),$(MONGOQUERY_CONFIG),$(HOME)/.mongoquery.yaml)
GO ?= go
LDFLAGS := -s -w

.PHONY: all build install uninstall run tidy fmt vet test vuln clean config help

all: build

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install: build
	mkdir -p $(INSTALL_DIR)
	install -m 0755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "installed: $(INSTALL_DIR)/$(BINARY)"

uninstall:
	rm -f $(INSTALL_DIR)/$(BINARY)

run: build
	./$(BINARY) $(ARGS)

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

vuln:
	$(GO) tool govulncheck ./...

clean:
	rm -f $(BINARY)

config:
	@if [ -e "$(CONFIG)" ] || [ -L "$(CONFIG)" ]; then \
		echo "config exists: $(CONFIG)"; \
	else \
		(umask 077; set -C; cat config.example.yaml > "$(CONFIG)") && \
		echo "created: $(CONFIG) — edit it before running"; \
	fi

help:
	@echo "targets:"
	@echo "  build      compile ./$(BINARY)"
	@echo "  install    copy binary to $(INSTALL_DIR)"
	@echo "  uninstall  remove installed binary"
	@echo "  run        build and run (ARGS='find --env ...')"
	@echo "  tidy       go mod tidy"
	@echo "  fmt        go fmt"
	@echo "  vet        go vet"
	@echo "  test       go test"
	@echo "  vuln       scan reachable dependencies for known vulnerabilities"
	@echo "  clean      remove built binary"
	@echo "  config     seed ~/.mongoquery.yaml from example"
