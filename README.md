# Easy Command

Windows 10/11 桌面脚本管理工具：将 `.bat` / `.sh` 入库，图形化配置参数并执行，实时查看输出，保存历史；支持应用内定时、全局快捷键与托盘常驻。

## 技术栈

- 桌面壳：Tauri 2 + Rust
- 前端：Vue 3 + TypeScript + Vite
- 本地执行引擎：Go 1.22+ Sidecar（stdin/stdout JSON Lines）
- 存储：MVP 使用 `%APPDATA%\easy-command\` 下的 JSON 文件

## 文档

- [架构设计](docs/ARCHITECTURE.md) — 目标与范围、分层职责、目录、生命周期、IPC、数据模型、调度与快捷键、更新与引导
- [分步开发计划](docs/DEV_PLAN.md) — 8 步目标、产出物、验收标准与待确认问题

## 当前状态

当前实现对应开发计划 **步骤 8：托盘、首次引导与 Windows 分发**。首次运行会引导检查数据目录、Git Bash 和密码保护。可在「设置」调整关窗行为及检查更新。

## 调度与全局快捷键

- 调度支持本地时间的五段 cron、每日时刻，以及 10～86400 秒的固定间隔。暂停保存在设置中，恢复后从未来的触发时刻继续。应用退出时调度停止。
- 同一脚本已在运行时，定时触发记为「已跳过」。启动前失败的原因可在执行历史查看。
- 快捷键使用最近选中的可用脚本。危险脚本由原生对话框确认。若组合键被占用，请在设置中更换。

## Windows 开发

准备 Node.js 22.13 或更高版本、pnpm 11、Go 1.22+、Rust stable `x86_64-pc-windows-msvc` 工具链、Visual Studio C++ 生成工具（含 Windows SDK）以及 Microsoft Edge WebView2 Runtime。安装说明见 [Tauri 开发环境](https://v2.tauri.app/start/prerequisites/)。

在仓库根目录运行：

```powershell
pnpm install
.\scripts\build-sidecar.ps1
pnpm tauri dev
```

构建脚本生成 `src-tauri/binaries/easy-command-engine-x86_64-pc-windows-msvc.exe`。开发模式会启动 Vite，监听 `127.0.0.1:1420`，并打开 **Easy Command**。在「导入脚本」中填写 `.bat` 或 `.sh` 的绝对路径，可选择同名时取消、重命名或覆盖。导入按原字节复制，UTF-8 正文可编辑，单个脚本最多 2 MiB。

前端构建检查运行 `pnpm --filter @easy-command/web build`。Go 测试在 `sidecar/` 中运行 `go test ./...`，覆盖同名策略、脚本编辑、删除状态清理和进程中止时的原子写入。Rust 协议测试在仓库根目录运行 `cargo test --locked --manifest-path src-tauri/Cargo.toml`。

`pnpm install` 会在首次安装时生成根目录的 `pnpm-lock.yaml`。只需更新锁文件时运行 `pnpm install --lockfile-only`，复现已有锁文件时运行 `pnpm install --frozen-lockfile`。

## 执行验证

- `.bat` 通过 Windows 的 `cmd.exe` 启动。批处理参数支持空格，双引号、百分号和换行会报参数错误。参数如何被再次解析取决于脚本本身。
- `.sh` 使用 Git for Windows 的 Bash。选择脚本后，在执行台的 Git Bash 面板探测或填写安装目录内的 `bin\bash.exe`。
- 输出默认按 Windows ANSI 代码页解码。在 `%APPDATA%\easy-command\config\settings.json` 中合并 `"outputEncoding": "utf-8"` 可切换。对 UTF-8 批处理输出，可在脚本中先执行 `chcp 65001 >nul`。
- 密码使用当前 Windows 用户的 DPAPI 保护。换用户、重装系统或迁移到另一台机器后，如无法解密，请重新设置。执行台只显示保存状态和 `***`。未保存时返回 `SECRET_NOT_FOUND`，无法解密时返回 `SECRET_DECRYPT_FAILED`。

在 `sidecar/` 下运行 `go test -count=1 -v ./internal/exec`，可验证批处理参数、流式输出、编码转换与取消时的子进程回收。

## 历史与日志

- 历史使用 `history/runs.jsonl`。按 runId 读取最后一条记录，Sidecar 重启时会为残留运行追加「中断」记录。
- 日志按运行开始日归档到 `logs/exec-YYYY-MM-DD.log`，可在历史详情查看。参数快照中的敏感值显示为 `***`。带密码参数的执行隐藏脚本输出，其他执行保存原文。
- 在「设置」可选不清理，或按 1～3650 天清理。保存后会触发清理，Sidecar 启动和运行期间每 6 小时也会检查。
- 在 `sidecar/` 下运行 `go test -count=1 -v ./internal/history ./internal/store`，可验证归并、恢复、保留和中途杀死原子重写进程。

## 仓库目录

- `apps/web/` 放置 Vue、TypeScript 和 Vite 前端。
- `src-tauri/` 放置 Tauri 桌面壳。
- `sidecar/` 放置 Go 引擎。
- `docs/` 放置架构与开发计划。
- `scripts/` 用于开发辅助脚本。

## Windows 安装与分发

安装包面向 Windows 10/11 x64，使用 NSIS 当前用户安装模式，安装向导可更改程序目录。Go 引擎随安装包提供。卸载会移除程序文件，`%APPDATA%\easy-command` 下的脚本、设置、历史和日志会保留。

安装包和更新包没有 Windows Authenticode 签名，首次运行或更新时可能出现 SmartScreen 警告。确认安装包来自可信的项目分发渠道后，若界面提供该选项，可点击「更多信息」再选择「仍要运行」。Tauri 更新使用独立的 minisign 签名校验安装包完整性。

`.bat` 使用 Windows 自带的 cmd.exe。`.sh` 需要单独安装 [Git for Windows](https://git-scm.com/install/windows)。引导会检测 Git Bash，也可手动指定安装目录下的 `bin\bash.exe`。密码密文由 Windows DPAPI 绑定当前用户。更换 Windows 用户、重装系统或迁移电脑后，如无法解密，请重新设置密码。

## 构建安装包与签名更新

维护者首次配置签名时，运行 `pnpm tauri signer generate --write-keys <仓库外路径>` 生成带口令的 minisign 密钥对。将对应公钥文件的内容配置到 `src-tauri/tauri.conf.json` 的 `plugins.updater.pubkey`。已发布的安装版本通过该公钥校验后续更新，发布后应保管并继续使用同一私钥。

在本地通过安全提示输入密钥口令。CI 中通过密钥存储注入相同的环境变量。

```powershell
$env:TAURI_SIGNING_PRIVATE_KEY = '<仓库外私钥的绝对路径>'
$signingPassword = Read-Host 'Updater 私钥口令' -AsSecureString
$env:TAURI_SIGNING_PRIVATE_KEY_PASSWORD = [System.Net.NetworkCredential]::new('', $signingPassword).Password
try {
    .\scripts\build-installer.ps1
} finally {
    Remove-Item Env:TAURI_SIGNING_PRIVATE_KEY_PASSWORD -ErrorAction SilentlyContinue
    Remove-Item Env:TAURI_SIGNING_PRIVATE_KEY -ErrorAction SilentlyContinue
}
```

如果此 Windows 用户的 `%LOCALAPPDATA%\EasyCommandSigning` 已保存项目密钥与 DPAPI 保护的口令，可将上例前两行换成以下代码。保留后面的构建和清理步骤。

```powershell
$signingDir = Join-Path $env:LOCALAPPDATA 'EasyCommandSigning'
$env:TAURI_SIGNING_PRIVATE_KEY = Join-Path $signingDir 'updater.key'
$signingPassword = Get-Content -LiteralPath (Join-Path $signingDir 'updater-password.dpapi') -Raw |
    ForEach-Object { ConvertTo-SecureString $_.Trim() }
```

脚本在 `src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/` 生成安装包及对应的 `.sig`。发布时将它们和 `latest.json` 一起上传。更新清单需要声明更高的应用版本，并在 `platforms.windows-x86_64` 中填写安装包 HTTPS 地址和 `.sig` 的完整内容。默认 feed 指向项目 GitHub Releases 的 `latest.json`，需要发布该文件后才可从默认地址检查更新。测试可用 `-ConfigOverride` 合并单独的版本和 endpoint 配置。
