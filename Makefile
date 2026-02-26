build:
	CGO_CFLAGS="-I$(CURDIR)/include" \
	CGO_LDFLAGS="-L$(CURDIR)/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread -Wl,-rpath,'$$ORIGIN/../lib/linux_amd64'" \
	go build -o bin/conductor ./cmd/conductor

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