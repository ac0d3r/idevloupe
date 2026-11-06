# iDevLoupe · iPhone 验机

纯 Go（`CGO_ENABLED=0`）的 iPhone 验机桌面程序，界面使用 [MyGo](https://github.com/egoist/mygo) Native UI。

```sh
make run      # 打开窗口
make test     # vet + 测试（含假 usbmuxd/设备的端到端测试）
make lint     # golangci-lint v2（需先安装）
make build    # 本机平台打包
make dist     # 全平台打包，可用 PLATFORMS=windows/amd64 指定
make i18n     # 改了翻译后重新生成并检查
make icon     # 重新生成图标
go run ./cmd/idevloupe -report   # 终端输出验机报告（-json 含原始数据，-list 列设备，-lang en|zh-Hans|de 指定语言）
SHOT_DIR=/tmp go test ./internal/gui   # 无窗口渲染界面截图
```

## 打包

产物在 `build/<os>-<arch>/`：macOS `.app` + `.dmg`；Windows `.exe`（装了 NSIS 时另有安装包）；
Linux 可执行文件、`.deb`、`.tar.gz`、PKGBUILD。配置在 `mygo.json`，图标 `resources/icon.png`
（`make icon` 重新生成）。

依赖系统的 usbmuxd：macOS 自带；Windows 需安装 iTunes / Apple 设备；Linux 需 `usbmuxd`。

## CI

- `.github/workflows/ci.yml`（push 到 main 与 PR）：golangci-lint、翻译文件已生成且有效、
  Linux / macOS / Windows 上 vet + 测试、Linux 上 `-race` 测试。
- `.github/workflows/build.yml`（push 到 main、`v*` 标签、手动触发）：在 macOS 上 `make dist` 打全平台包，
  上传为 artifact；推 `vX.Y.Z` 标签时另发布 GitHub Release，附 `.dmg`、Windows `.exe`、`.deb`、
  `.tar.gz` 与 `SHA256SUMS`。版本号取自标签，打包前自动写入 `mygo.json`，无需手动修改。
  安装包未签名 / 公证。

## 下载页

`docs/` 是一个静态下载页（`index.html` 单文件，中 / 英 / 德，按访客系统推荐安装包），
在仓库 Settings → Pages 选 `main` 分支的 `/docs` 即可发布。下载链接指向
`releases/latest/download/<文件名>`，因此 Build 发布的文件名不带版本号。
截图来自测试中的虚构设备，`make screenshots` 重新生成。

## 多语言

支持简体中文、English、Deutsch。默认跟随系统语言，侧边栏底部可切换，选择会被记住；
切换后报告用已读到的数据按新语言重新生成，无需重新检测。命令行默认按 `LANG` 选择，或用 `-lang` 指定。

所有文字在 `internal/locales/{en,zh-Hans,de}.json`（ARB 格式，ICU MessageFormat，英文为源语言，
缺失的翻译回退到英文）。改完运行 `make i18n` 生成 `catalog_gen.go`。测试会检查：

- Go 代码里不再出现中文字面量，界面文字必须走翻译文件；
- 翻译文件里的每条文字都被代码用到；
- 同一份数据在三种语言下的检查项与状态完全一致，英 / 德报告不夹带中文。

导出的 JSON 中，检查项的 `Status` 是与语言无关的代码（`pass` / `info` / `warn` / `fail` / `unknown`），
`StatusText` 才是对应语言的文字。

## 结构

`pkg/` 是与设备通信的库，不依赖应用本身，可单独使用（`github.com/ac0d3r/idevloupe/pkg/...`）；
`internal/` 是应用：检测规则、界面与文字。

| 包 | 作用 |
|---|---|
| `pkg/plistx` | plist 帧读写与松散类型取值 |
| `pkg/usbmux` | usbmuxd 协议：设备列表、热插拔、配对记录、端口连接 |
| `pkg/lockdown` | lockdownd：GetValue、StartSession（TLS）、StartService、配对（证书生成） |
| `pkg/services` | diagnostics_relay（IORegistry/All/MobileGestalt）、installation_proxy、MCInstall、服务探测、AFC 与系统分析日志（crashreportmover / crashreportcopymobile） |
| `pkg/device` | 打开受信任的会话、配对、读取基本信息 |
| `internal/inspect` | 采集 → 分析 → 报告（纯函数，可用录制数据测试） |
| `internal/gui` | MyGo 界面 |
| `internal/locales` | 三种语言的文字（JSON）与生成的 Go 函数 |
| `internal/probe` | 逐项查询 IORegistry（`-dump-ioreg`），用于寻找新的部件字段 |
| `internal/fakedevice` | 测试用的假 usbmuxd + 假 iPhone |

协议实现参考 go-ios（MIT）、ipsw `pkg/usb`（MIT）与 libimobiledevice，只取 lockdown 通道需要的部分，
不依赖 go-ios 本身，因此不会引入 gopacket / gvisor / tun 等依赖。

## 检测原则

- 只有拿到正面证据才判「正常」；数据读不到或检查不完整一律「未知」，不会默认通过。
- 回复与请求逐一核对（请求类型、服务名、domain/key、AFC 包序号），连接出错即作废重建，避免回复错位导致误判。
- 每项结论都附带依据；「原始数据」页展示设备返回的全部原值，便于复核。
- 软件无法可靠判断的（部件是否原装、保修、运营商锁、外观等）列入「需人工核对」。
