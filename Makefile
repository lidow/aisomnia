BIN     := aisomnia
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test lint vuln check fmt clean help

build: ## build ./aisomnia
	go build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/aisomnia

install: build ## install into $(PREFIX)/bin
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BIN)
	@echo "installed $(PREFIX)/bin/$(BIN); make sure $(PREFIX)/bin is on your PATH"

test: ## unit tests and vet (never touch macOS settings)
	go vet ./...
	go test -race -count=1 ./...

lint: ## golangci-lint (brew install golangci-lint)
	golangci-lint run ./...

vuln: ## govulncheck (go install golang.org/x/vuln/cmd/govulncheck@latest)
	govulncheck ./...

fmt:
	gofmt -w .

check: fmt test lint ## what CI runs

clean:
	rm -rf $(BIN) dist

help:
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed 's/:.*## /  /'
