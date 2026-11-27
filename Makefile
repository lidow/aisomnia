BIN     := aisomnia
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test lint vuln check fmt clean set-owner set-name

build: ## build ./aisomnia
	go build -ldflags '$(LDFLAGS)' -o $(BIN) .

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

# One-time setup before the first push: put the GitHub account into every placeholder.
#   make set-owner OWNER=your-github-account
set-owner:
	@test -n "$(OWNER)" || { echo "usage: make set-owner OWNER=<github account>"; exit 2; }
	@git grep -lz 'OWNER' -- ':!Makefile' | xargs -0 sed -i '' 's#github.com/OWNER/#github.com/$(OWNER)/#g; s#OWNER/tap/#$(OWNER)/tap/#g; s#owner: OWNER#owner: $(OWNER)#g; s#@OWNER#@$(OWNER)#g; s#OWNER/homebrew-tap#$(OWNER)/homebrew-tap#g'
	@git grep -n 'OWNER' -- ':!Makefile' || echo "all placeholders replaced with $(OWNER)"

# Rename the project everywhere (module path, binary, label, config path, docs).
#   make set-name NAME=newname
set-name:
	@test -n "$(NAME)" || { echo "usage: make set-name NAME=<name>"; exit 2; }
	@git grep -lz 'aisomnia' -- ':!Makefile' ':!CODE_OF_CONDUCT.md' | xargs -0 sed -i '' 's#aisomnia#$(NAME)#g'
	@git mv launchd/dev.aisomnia.lidwatch.plist launchd/dev.$(NAME).lidwatch.plist
	@git mv aisomnia_test.go $(NAME)_test.go
	@sed -i '' 's#aisomnia#$(NAME)#g' Makefile
	@echo "renamed to $(NAME); review git diff, then run make check"

help:
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed 's/:.*## /  /'
