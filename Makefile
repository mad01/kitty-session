BINARY_NAME := ks
CMD_PATH := ./cmd/ks
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

.PHONY: build test install clean fmt lint

build:
	@go build -ldflags "-X github.com/mad01/kitty-session/internal/cli.Version=$(GIT_COMMIT)" \
		-o $(BINARY_NAME) $(CMD_PATH)

test:
	@go test -timeout 30s ./...

install: build
	@mkdir -p ~/code/bin
	@cp $(BINARY_NAME) ~/code/bin/$(BINARY_NAME)
	@xattr -dr com.apple.provenance ~/code/bin/$(BINARY_NAME) 2>/dev/null || true
	@codesign -fs - ~/code/bin/$(BINARY_NAME)
	@echo "installed $(BINARY_NAME) to ~/code/bin/$(BINARY_NAME)"

clean:
	@rm -f $(BINARY_NAME)

fmt:
	@gofmt -w .

lint:
	@golangci-lint run ./...
