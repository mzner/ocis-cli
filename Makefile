.PHONY: build check clean coverage fmt install integration integration-down \
	integration-logs integration-test integration-up lint race test

VERSION ?= dev
COVERAGE_MIN ?= 75
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
		app auth graph httpapi search sharing sync trash transfer versions webdav

fmt:
	gofmt -w .

lint:
	go tool golangci-lint run ./...

race:
	go test -race ./...

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
