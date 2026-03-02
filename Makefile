#build:
#	CGO_CFLAGS="-I$(CURDIR)/include" \
#	CGO_LDFLAGS="-L$(CURDIR)/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread -Wl,-rpath,'$$ORIGIN/../lib/linux_amd64'" \
#	go build -o bin/conductor ./cmd/conductor

build:
	CGO_CFLAGS="-I$(CURDIR)/include" \
	CGO_LDFLAGS="-L$(CURDIR)/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread -Wl,-rpath,$(CURDIR)/lib/linux_amd64" \
	go build -o bin/conductor ./cmd/conductor

test:
	CGO_CFLAGS="-I$(CURDIR)/include" \
	CGO_LDFLAGS="-L$(CURDIR)/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread" \
	LD_LIBRARY_PATH=$(CURDIR)/lib/linux_amd64 \
	go test ./...

smoketest:
	CGO_CFLAGS="-I$(CURDIR)/include" \
	CGO_LDFLAGS="-L$(CURDIR)/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread" \
	LD_LIBRARY_PATH=$(CURDIR)/lib/linux_amd64 \
	go run ./cmd/smoketest/

smokeparser:
	CGO_CFLAGS="-I$(CURDIR)/include" \
	CGO_LDFLAGS="-L$(CURDIR)/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread" \
	LD_LIBRARY_PATH=$(CURDIR)/lib/linux_amd64 \
	go run ./cmd/smoke-rag-parser/

smokesearch:
	CGO_CFLAGS="-I$(CURDIR)/include" \
	CGO_LDFLAGS="-L$(CURDIR)/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread" \
	LD_LIBRARY_PATH=$(CURDIR)/lib/linux_amd64 \
	go run ./cmd/smoke-search/

index:
    LD_LIBRARY_PATH=$(CURDIR)/lib/linux_amd64 \
    ./bin/conductor index --project ./project.yaml