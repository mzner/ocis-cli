.PHONY: build check clean coverage fmt install integration integration-down \
	integration-logs integration-test integration-up lint race release-check \
	release release-smoke release-snapshot secrets test vuln

VERSION ?= dev
COVERAGE_MIN ?= 75
GORELEASER ?= goreleaser
GITLEAKS_VERSION ?= v8.30.1
GOVULNCHECK_VERSION ?= v1.6.0
LDFLAGS := -X github.com/mzner/ocis-cli/internal/app.Version=$(VERSION)
OCIS_INTEGRATION_VERSION ?= 8.1.0
OCIS_INTEGRATION_PORT ?= 9200
OCIS_INTEGRATION_SERVER ?= https://localhost:$(OCIS_INTEGRATION_PORT)
OCIS_INTEGRATION_PROJECT ?= ocis-cli-it-$(subst .,-,$(OCIS_INTEGRATION_VERSION))
INTEGRATION_COMPOSE := OCIS_INTEGRATION_VERSION=$(OCIS_INTEGRATION_VERSION) \
	OCIS_INTEGRATION_PORT=$(OCIS_INTEGRATION_PORT) \
	OCIS_INTEGRATION_SERVER=$(OCIS_INTEGRATION_SERVER) \
	docker compose --project-name $(OCIS_INTEGRATION_PROJECT) \
	--file test/integration/docker-compose.yml

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/ocis ./cmd/ocis

install: build
	mkdir -p $(HOME)/.local/bin
	cp bin/ocis $(HOME)/.local/bin/ocis

test:
	go test ./...

check: fmt
	go test ./...
	go test -race ./...
	$(MAKE) coverage
	go vet ./...
	$(MAKE) lint

coverage:
	go run ./tools/covercheck -min $(COVERAGE_MIN) \
		app auth graph httpapi retry search sharing sync trash transfer versions \
		webdav

fmt:
	gofmt -w .

lint:
	go tool golangci-lint run ./...

race:
	go test -race ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

secrets:
	go run github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION) \
		git --redact --no-banner .

release-check:
	@test -z "$$(git status --porcelain)" || { \
		echo "release preparation requires a clean working tree" >&2; exit 1; \
	}
	@command -v "$(GORELEASER)" >/dev/null || { \
		echo "goreleaser is required" >&2; exit 1; \
	}
	@command -v syft >/dev/null || { \
		echo "syft is required" >&2; exit 1; \
	}
	$(GORELEASER) check
	$(MAKE) vuln
	$(MAKE) secrets

release-snapshot: release-check
	$(GORELEASER) release --snapshot --clean
	go run ./tools/releaseformula --dist dist
	$(MAKE) release-smoke

release-smoke:
	go run ./tools/releasesmoke --dist dist

release:
	@printf '%s\n' "$(VERSION)" | \
		grep -Eq '^[1-9][0-9]*\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$$' || { \
		echo "VERSION must be a stable semantic version such as 1.0.0" >&2; \
		exit 1; \
	}
	git fetch origin main --tags
	@test "$$(git branch --show-current)" = main || { \
		echo "releases must be created from main" >&2; exit 1; \
	}
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || { \
		echo "local main must exactly match origin/main" >&2; exit 1; \
	}
	@test -z "$$(git tag --list "v$(VERSION)")" || { \
		echo "tag v$(VERSION) already exists" >&2; exit 1; \
	}
	@if ! git tag --list 'v*' | grep -q . && [ "$(VERSION)" != 1.0.0 ]; then \
		echo "the first release must be VERSION=1.0.0" >&2; exit 1; \
	fi
	$(MAKE) release-snapshot
	$(MAKE) integration
	git tag -s "v$(VERSION)" -m "ocis-cli v$(VERSION)"
	git push origin "v$(VERSION)"

integration-up:
	$(INTEGRATION_COMPOSE) up --detach --wait
	go run ./test/integration/cmd/wait \
		--url "$(OCIS_INTEGRATION_SERVER)" --insecure --timeout 5m

integration-test: build
	OCIS_INTEGRATION=1 \
	OCIS_INTEGRATION_BINARY="$(CURDIR)/bin/ocis" \
	OCIS_INTEGRATION_SERVER="$(OCIS_INTEGRATION_SERVER)" \
	OCIS_INTEGRATION_INSECURE=true \
	go test -count=1 -timeout=20m -v ./test/integration

integration:
	@set -eu; \
	trap '$(MAKE) integration-down \
		OCIS_INTEGRATION_VERSION=$(OCIS_INTEGRATION_VERSION) \
		OCIS_INTEGRATION_PORT=$(OCIS_INTEGRATION_PORT) \
		OCIS_INTEGRATION_SERVER=$(OCIS_INTEGRATION_SERVER) \
		OCIS_INTEGRATION_PROJECT=$(OCIS_INTEGRATION_PROJECT)' EXIT INT TERM; \
	$(MAKE) integration-up \
		OCIS_INTEGRATION_VERSION=$(OCIS_INTEGRATION_VERSION) \
		OCIS_INTEGRATION_PORT=$(OCIS_INTEGRATION_PORT) \
		OCIS_INTEGRATION_SERVER=$(OCIS_INTEGRATION_SERVER) \
		OCIS_INTEGRATION_PROJECT=$(OCIS_INTEGRATION_PROJECT); \
	$(MAKE) integration-test OCIS_INTEGRATION_SERVER=$(OCIS_INTEGRATION_SERVER)

integration-logs:
	@$(INTEGRATION_COMPOSE) logs --no-color 2>&1 | \
		go run ./test/integration/cmd/sanitize

integration-down:
	$(INTEGRATION_COMPOSE) down --volumes --remove-orphans

clean:
	rm -f bin/ocis
	rm -rf dist
