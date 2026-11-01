# Developer shortcuts. The real commands are plain `go`; nothing here is required.
GO      ?= go
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-dev build-release test vet dev seed bench contract clean

build:            ## default build: no dev login, no embedded front end
	CGO_ENABLED=1 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/dashboard ./cmd/dashboard

build-dev:        ## dev build: adds /auth/dev (also needs DEV_AUTH=1)
	CGO_ENABLED=1 $(GO) build -tags dev -o bin/dashboard-dev ./cmd/dashboard

build-release:    ## release build: embeds ../web/dist (run the front-end build first)
	rm -rf internal/web/dist && cp -r ../web/dist internal/web/dist
	CGO_ENABLED=1 $(GO) build -trimpath -tags embedweb -ldflags "$(LDFLAGS)" -o bin/dashboard ./cmd/dashboard

test:
	$(GO) test ./...
	$(GO) test -tags dev ./internal/api

vet:
	$(GO) vet ./...
	$(GO) vet -tags dev ./...

dev: build-dev    ## seeded dev server on :8090 serving ../web/dist
	DB_PATH=./data/dev.db ./bin/dashboard-dev seed -reset
	DEV_AUTH=1 DB_PATH=./data/dev.db STATIC_DIR=../web/dist ./bin/dashboard-dev serve

seed:
	DB_PATH=./data/dev.db $(GO) run ./cmd/dashboard seed -reset

bench:
	$(GO) test -run '^$$' -bench . -benchmem -benchtime 2s ./internal/api

contract:
	node ../scripts/contract-check.mjs

clean:
	rm -rf bin data internal/web/dist
