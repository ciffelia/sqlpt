# Install command line tools
.PHONY: install
install:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/eabc2638a66daf5bb6c6fb052a32fa3ef7b6600d/install.sh | sh -s -- -b "$(shell go env GOPATH)/bin" v2.1.6

# Format code, run static analysis, and run tests
.PHONY: check
check: tidy format lint test

# Clean up module dependencies
.PHONY: tidy
tidy:
	go mod tidy

# Format code
.PHONY: format
format:
	go fmt ./...

# Run static analysis
.PHONY: lint
lint:
	go vet ./...
	golangci-lint run

# Run tests
.PHONY: test
test:
	go test ./...

# Run tests with verbose output
.PHONY: test-verbose
test-verbose:
	go test -v ./...

# Run tests with race detector
.PHONY: test-race
test-race:
	go test -race ./...
