GOCACHE ?= /tmp/agent-conductor-gocache
GOMODCACHE ?= /tmp/agent-conductor-gomodcache

CGO_CFLAGS_COMMON := -I$(CURDIR)/include
CGO_LDFLAGS_COMMON := -L$(CURDIR)/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread
GO_ENV := GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE)
CGO_ENV := CGO_CFLAGS="$(CGO_CFLAGS_COMMON)" CGO_LDFLAGS="$(CGO_LDFLAGS_COMMON)"

.PHONY: build web-build web-test observability-verify test smoketest smokeparser smokesearch index sqlc

#build:
#	CGO_CFLAGS="-I$(CURDIR)/include" \
#	CGO_LDFLAGS="-L$(CURDIR)/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread -Wl,-rpath,'$$ORIGIN/../lib/linux_amd64'" \
#	go build -o bin/conductor ./cmd/conductor

build:
	$(GO_ENV) \
	CGO_CFLAGS="$(CGO_CFLAGS_COMMON)" \
	CGO_LDFLAGS="$(CGO_LDFLAGS_COMMON) -Wl,-rpath,$(CURDIR)/lib/linux_amd64" \
	go build -o bin/conductor ./cmd/conductor

web-build:
	cd web && npm run build

web-test:
	cd web && npm run test

observability-verify: web-test web-build test

test:
	$(GO_ENV) \
	$(CGO_ENV) \
	LD_LIBRARY_PATH=$(CURDIR)/lib/linux_amd64 \
	go test ./...

smoketest:
	$(GO_ENV) \
	$(CGO_ENV) \
	LD_LIBRARY_PATH=$(CURDIR)/lib/linux_amd64 \
	go run ./cmd/smoketest/

smokeparser:
	$(GO_ENV) \
	$(CGO_ENV) \
	LD_LIBRARY_PATH=$(CURDIR)/lib/linux_amd64 \
	go run ./cmd/smoke-rag-parser/

smokesearch:
	$(GO_ENV) \
	$(CGO_ENV) \
	LD_LIBRARY_PATH=$(CURDIR)/lib/linux_amd64 \
	go run ./cmd/smoke-search/

index:
	LD_LIBRARY_PATH=$(CURDIR)/lib/linux_amd64 \
	./bin/conductor index --project ./project.yaml

sqlc:
	sqlc generate
