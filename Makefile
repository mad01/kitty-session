BINARY_NAME := ks
CMD_PATH := ./cmd/ks
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

.PHONY: build test install clean fmt lint demo demo-split

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
	@golines -m 100 --base-formatter=gofumpt -w .

lint:
	@golangci-lint run ./...

# Re-render images/sidebar.gif and the README stills from demo/sidebar.tape.
# Needs vhs, ttyd and ffmpeg on PATH; the tape runs the branch build of ks.
demo: build
	@PATH="$(CURDIR):$$PATH" vhs demo/sidebar.tape

# Re-record images/split.webp, a real tab from a throwaway instance; see
# demo/split.sh for what it needs (kitty with Screen Recording permission).
demo-split: build
	@zsh demo/split.sh
