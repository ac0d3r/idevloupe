# iDevLoupe

纯 Go（`CGO_ENABLED=0`）的 iPhone 设备管理桌面工具，界面使用 [MyGo](https://github.com/egoist/mygo)。
目前提供验机，原理与代码结构见 [docs/inspection.md](docs/inspection.md)。支持简体中文、English、Deutsch。

下载：[GitHub Releases](https://github.com/ac0d3r/idevloupe/releases/latest)。依赖系统的 usbmuxd：
macOS 自带；Windows 需安装 Apple 设备或 iTunes；Linux 需 `usbmuxd`。

## 开发

```sh
make run          # 打开窗口
make test         # vet + 测试（含假 usbmuxd / 设备的端到端测试）
make lint         # golangci-lint v2
make build        # 本机平台打包，产物在 build/<os>-<arch>/
make dist         # 全平台打包，可用 PLATFORMS=windows/amd64 指定
make i18n         # 改了 internal/locales/*.json 后重新生成
make screenshots  # 重新生成下载页截图
go run ./cmd/idevloupe -report   # 终端输出验机报告（-json / -list / -lang）
```

推送 `vX.Y.Z` 标签会打包并发布 Release，版本号取自标签。`docs/` 是 GitHub Pages 下载页。
