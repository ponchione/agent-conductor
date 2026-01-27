build:
	go mod tidy
	go build -o conductor ./cmd/conductor