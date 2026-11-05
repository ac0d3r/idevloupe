export GOWORK      := off
export CGO_ENABLED := 0

PLATFORMS ?= darwin/universal,windows/amd64,windows/arm64,linux/amd64,linux/arm64

.PHONY: run test lint build dist i18n icon screenshots clean

run:                ## 打开窗口
	go run ./cmd/idevloupe

test:               ## vet + 测试
	go vet ./...
	go test ./...

lint:               ## golangci-lint（配置见 .golangci.yml）
	golangci-lint run ./...

build:              ## 本机平台打包
	go tool mygo build

dist:               ## 全平台打包（PLATFORMS 可覆盖）
	go tool mygo build -platform $(PLATFORMS)

i18n:               ## 改了翻译后：重新生成 internal/locales 的 Go 代码并检查
	go tool mygo generate
	go tool mygo i18n check

icon:               ## 重新生成 resources/icon.png
	ICON_OUT=$(CURDIR)/resources/icon.png go test -count=1 -run TestIcon ./internal/gui

screenshots:        ## 重新生成下载页截图 docs/screenshot-*.png（虚构设备）
	SHOT_DIR=$(CURDIR)/docs go test -count=1 -run TestScreenshots ./internal/gui

clean:
	rm -rf build
