# TG WS Proxy Makefile

BINARY_NAME=TgWsProxy
VERSION=1.1.3
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all build clean test windows linux darwin android

all: windows linux darwin

build: windows

windows:
	@echo "Building for Windows..."
	@go build $(LDFLAGS) -o $(BINARY_NAME).exe ./cmd/proxy
	@echo "Built: $(BINARY_NAME).exe"

linux:
	@echo "Building for Linux..."
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)_linux ./cmd/proxy
	@echo "Built: $(BINARY_NAME)_linux"

darwin:
	@echo "Building for macOS..."
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)_macos_amd64 ./cmd/proxy
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)_macos_arm64 ./cmd/proxy
	@echo "Built: $(BINARY_NAME)_macos_amd64, $(BINARY_NAME)_macos_arm64"

android:
	@echo "Building for Android..."
	@cd mobile && gomobile bind -target android -o ../android/tgwsproxy.aar ./mobile
	@echo "Built: android/tgwsproxy.aar"
	@echo "See android/README.md for APK build instructions"

test:
	@echo "Running tests..."
	@go test -v ./internal/...

clean:
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME)* 2>/dev/null || true
	@rm -rf bin/ 2>/dev/null || true
	@go clean
	@echo "Cleaned"

run:
	@go run ./cmd/proxy -v

install:
	@go install ./cmd/proxy

tidy:
	@go mod tidy
