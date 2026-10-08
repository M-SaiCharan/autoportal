VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GO      ?= go
BUILD   := CGO_ENABLED=0 $(GO) build -trimpath
# The console build updates itself from its own release file.
CLI_ASSET := github.com/M-SaiCharan/autoportal/internal/update.Asset=autoportal-cli-windows-amd64.exe

.PHONY: all test vet build assets dist macos clean

all: vet test build

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...
	GOOS=windows $(GO) vet ./...

# Binary for the machine you're on.
build:
	CGO_ENABLED=1 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/autoportal ./cmd/autoportal

# Icons and the Windows .exe resources (icon, manifest, version info).
assets:
	$(GO) run ./tools/genassets -version $(VERSION)

# Everything that can be built from Linux (macOS needs a Mac: make macos).
dist: assets
	GOOS=linux   GOARCH=amd64 $(BUILD) -ldflags "$(LDFLAGS)" -o dist/autoportal-linux-amd64 ./cmd/autoportal
	GOOS=linux   GOARCH=arm64 $(BUILD) -ldflags "$(LDFLAGS)" -o dist/autoportal-linux-arm64 ./cmd/autoportal
	GOOS=windows GOARCH=amd64 $(BUILD) -ldflags "$(LDFLAGS) -H windowsgui" -o dist/autoportal-windows-amd64.exe ./cmd/autoportal
	GOOS=windows GOARCH=arm64 $(BUILD) -ldflags "$(LDFLAGS) -H windowsgui" -o dist/autoportal-windows-arm64.exe ./cmd/autoportal
	GOOS=windows GOARCH=amd64 $(BUILD) -ldflags "$(LDFLAGS) -X $(CLI_ASSET)" -o dist/autoportal-cli-windows-amd64.exe ./cmd/autoportal

macos:
	sh scripts/package-macos.sh $(VERSION)

clean:
	rm -rf dist cmd/autoportal/*.syso
