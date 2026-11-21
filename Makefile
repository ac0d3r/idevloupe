export GOWORK      := off
export CGO_ENABLED := 0

PLATFORMS ?= darwin/universal,windows/amd64,windows/arm64,linux/amd64,linux/arm64

.PHONY: run test lint build dist i18n icon screenshots clean

run:
	go run ./cmd/idevloupe

test:
	go vet ./...
	go test ./...

lint:
	golangci-lint run ./...

build:
	go tool mygo build

dist:
	go tool mygo build -platform $(PLATFORMS)

i18n:
	go tool mygo generate
	go tool mygo i18n check

icon:
	ICON_OUT=$(CURDIR)/resources/icon.png go test -count=1 -run TestIcon ./internal/gui

screenshots:
	SHOT_DIR=$(CURDIR)/docs go test -count=1 -run TestScreenshots ./internal/gui

clean:
	rm -rf build
