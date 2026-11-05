# GoBingWallpaper（Windows）

这是 PyBingWallpaper 的 Windows-only Go 重写。下载、配置和数据结构尽量与
Python 1.6.0 兼容，同时按 Go 的方式拆分为配置、Bing 客户端、下载、历史、
SQLite 归档和 Windows 平台适配层。

## 已实现功能

- Bing 每日壁纸，支持 `country`、`market`、日期偏移和 global/china/custom 服务；
- `prefer/highest`、`insist`、`manual`、`never`、`uhd` 分辨率策略；
- `accompany` 附加图片收集（Go 版不再下载 MP4 视频）；
- 前台单次运行、后台定时检查、网络错误指数退避重试（1 分钟起，最长 30 分钟）；
- 原版 `settings.conf` INI 分区和“默认值 < 配置文件 < CLI”优先级；
- 原版 JSON 历史路径与字段兼容，支持 `--redownload`；
- 可选 SQLite 5.6.1 表结构，支持在数据库中嵌入图片或仅存元数据；
- HTTP/HTTPS 代理及 Basic 代理认证；
- 通过 Win32 `SystemParametersInfoW` 设置壁纸，并备份原注册表壁纸值；
- 通过当前用户 `HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run`
  管理自启动，不需要管理员权限。
- 主图和伴随图使用不同文件名，避免收集资源相互覆盖；
- 同一壁纸当天只设置一次，并跳过重复的 BMP 转换和设置调用；
- 通过 Windows 命名互斥量防止多个实例同时修改壁纸和历史文件。

全部依赖都是 Go 或纯 Go 实现。项目不使用 CGO，也不需要 MSVC、MinGW、C++
运行库或 Python。SQLite 使用 `modernc.org/sqlite` 的无 CGO 驱动。

## 构建

需要 Go 1.25 或更高版本。在 Windows PowerShell 中运行：

```powershell
.\build-windows.ps1
```

脚本会生成无控制台窗口的 `BingWallpaper.exe` 和用于命令行管理的
`BingWallpaper-cli.exe`。GUI 版本的日志位于
`%LOCALAPPDATA%\\zhimoe\\BingWallpaper\\bingwallpaper.log`。

或者从任意平台交叉编译 64 位 Windows CLI 版本：

```text
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o dist/BingWallpaper-cli.exe ./cmd/bingwallpaper
```

## 快速使用

```powershell
# 下载并设置今天的壁纸
.\dist\BingWallpaper-cli.exe

# 生成默认 settings.conf
.\dist\BingWallpaper-cli.exe --generate-config

# 选择中国市场并保留所有文件
.\dist\BingWallpaper-cli.exe --market zh-CN --keep-file-name

# 仅下载，不修改桌面
.\dist\BingWallpaper-cli.exe --setter no

# 后台驻留，每 2 小时检查一次
.\dist\BingWallpaper-cli.exe --background --interval 2
```

默认图片目录是 `%USERPROFILE%\\MyBingWallpapers`，历史文件仍为
`%APPDATA%\\Genzj\\PyBingWallpaper\\bing-wallpaper-history.json`。默认配置文件
是可执行程序同目录的 `settings.conf`。

命中下载历史时会复用本地图片；如果同一 URL 和本地文件今天已经成功设置过，
则跳过设置调用，避免桌面和任务栏重复刷新。设置状态保存在
`%LOCALAPPDATA%\\zhimoe\\BingWallpaper\\state.json`，与 Python 兼容的下载历史分开，
不会影响原版程序。`--setter no` 仅复用文件，不修改桌面，也不记录设置状态。
`--redownload` 会忽略该状态，强制重新下载并设置。
已丢失、为空或变成目录的缓存文件会重新下载；使用固定文件名时，覆盖图片会同时
移除该路径对应的旧历史记录。

## 自启动

GUI 版 `BingWallpaper.exe` 是自安装的：双击运行时若尚未注册自启动，会
自动注册为登录后后台驻留（`background` 模式），然后继续执行正常的壁纸更新，
无需任何命令行参数。CLI 版不会自动注册，也不弹出任何提示。

自启动管理命令执行完即退出：

```powershell
# 登录后驻留并定时更新
.\dist\BingWallpaper-cli.exe --startup-mode background

# 登录后只更新一次
.\dist\BingWallpaper-cli.exe --startup-mode foreground

# 查看或禁用
.\dist\BingWallpaper-cli.exe --startup-mode status
.\dist\BingWallpaper-cli.exe --startup-mode disable
```

这些命令仅改当前用户的注册表，不请求管理员权限。移动 exe 后应重新执行一次
启用命令，使注册表中的绝对路径同步更新。通过 `BingWallpaper-cli.exe` 启用时，
如果同目录存在 GUI 版 `BingWallpaper.exe`，注册表会自动指向 GUI 版，登录后不会
弹出常驻控制台窗口。

## 配置兼容

可直接复制原版 `settings.conf`。完整示例见
[`settings.example.conf`](settings.example.conf)。CLI 参数继续覆盖配置文件；
`--foreground` 会取消配置中的 `background=true`。
配置中的相对 `output_folder` 和 `database_file` 都相对于配置文件所在目录解析。

原版选项中只有以下行为有意调整：

- 只支持 `setter=win/no`，不再加载 Python 外部 setter；`setter_args` 仅解析保留；
- 不再下载 `video`/`hdvideo` MP4 资源；旧配置中的这些 `collect` 值会被忽略并提示；
- 代理用户名/密码使用 HTTP Basic 认证，不实现旧 Python 代码中的 NTLM/Digest
  握手；不需要认证的 HTTP/HTTPS 代理不受影响；
- 自启动由程序直接管理 HKCU Run 注册表，不依赖安装器创建 Startup 快捷方式；
- 使用 HTTPS 访问 Bing 默认服务。

查看完整 CLI：

```powershell
.\dist\BingWallpaper-cli.exe --help
```

## 验证

```text
go test ./...
go vet ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build ./...
```
