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

已实现托盘、首次运行引导和更新入口。前端生产构建、Rust 测试与 x64 Release 编译已验证。默认打包不生成 updater 签名产物，可直接打 NSIS / MSI；应用内自动更新可选，需要时再单独配置签名。

## 调度与全局快捷键

- 调度支持本地时间的五段 cron、每日时刻，以及 10～86400 秒的固定间隔。暂停保存在设置中，恢复后从未来的触发时刻继续。应用退出时调度停止。
- 同一脚本已在运行时，定时触发记为「已跳过」。启动前失败的原因可在执行历史查看。
- 快捷键使用最近选中的可用脚本。危险脚本由原生对话框确认。若组合键被占用，请在设置中更换。

## Windows 开发

在 Windows 10/11 x64 上准备以下工具。

- Node.js 24 LTS，或 Node.js 22.13+ 的 22.x 版本。pnpm 使用根 `package.json` 固定的 `11.22.0`，可执行 `npm install -g pnpm@11.22.0` 安装。
- Go 1.22+ 和 Rust stable `x86_64-pc-windows-msvc` 工具链。可通过 `rustup toolchain install stable-x86_64-pc-windows-msvc` 安装 Rust 工具链。
- Visual Studio Build Tools 的「使用 C++ 的桌面开发」工作负载（含 Windows SDK），以及 Microsoft Edge WebView2 Runtime。 [Tauri 开发环境](https://v2.tauri.app/start/prerequisites/)
- 如需执行 `.sh`，安装 [Git for Windows](https://git-scm.com/install/windows)。

在仓库根目录用 PowerShell 运行。

```powershell
pnpm install --frozen-lockfile
.\scripts\build-sidecar.ps1
pnpm tauri dev
```

构建脚本生成 `src-tauri/binaries/easy-command-engine-x86_64-pc-windows-msvc.exe`。Tauri 自动启动 Vite，在 `127.0.0.1:1420` 提供前端并打开主窗口。Go 代码改动后，从托盘退出应用，停止原来的开发命令，再重新编译 Sidecar 并启动。更新依赖后用 `pnpm install --lockfile-only` 刷新根 `pnpm-lock.yaml`，将依赖声明与锁文件一起提交。

### 测试与冒烟检查

完成上面的依赖安装和 Sidecar 编译后，在仓库根目录执行。

```powershell
pnpm --filter @easy-command/web build
cargo fmt --check --manifest-path src-tauri/Cargo.toml
cargo test --locked --manifest-path src-tauri/Cargo.toml
Push-Location sidecar
try { go test ./... } finally { Pop-Location }
```

将以下内容保存为 `hello.bat`，在脚本库中填写它的绝对路径导入并运行。预期执行台显示 `Easy Command smoke test`、退出码 `0`，历史页新增一条成功记录。

```bat
@echo off
echo Easy Command smoke test
exit /b 0
```

导入会保留源文件，并按原字节复制到库中。同名时可选择取消、重命名或覆盖。UTF-8 正文可在应用内编辑，单个脚本最多 2 MiB。

## 脚本执行

- `.bat` 通过 Windows 的 `cmd.exe` 启动。批处理参数支持空格，双引号、百分号和换行会报参数错误。参数如何被再次解析取决于脚本本身。
- `.sh` 使用 Git for Windows 的 Bash。选择脚本后，在执行台的 Git Bash 面板探测或填写安装目录内的 `bin\bash.exe`。
- 输出默认按 Windows ANSI 代码页解码。若需切换，先从托盘退出，再在 `%APPDATA%\easy-command\config\settings.json` 中合并 `"outputEncoding": "utf-8"` 并重新启动。对 UTF-8 批处理输出，可在脚本中先执行 `chcp 65001 >nul`。
- 密码使用当前 Windows 用户的 DPAPI 保护。换用户、重装系统或迁移到另一台机器后，如无法解密，请重新设置。执行台只显示保存状态和 `***`。未保存时返回 `SECRET_NOT_FOUND`，无法解密时返回 `SECRET_DECRYPT_FAILED`。

在 `sidecar/` 下运行 `go test -count=1 -v ./internal/exec`，可验证批处理参数、流式输出、编码转换与取消时的子进程回收。

## 用户数据、历史与日志

用户数据位于 `%APPDATA%\easy-command`。开发版和安装版在同一账户的默认配置下使用同一目录。手工验收可用另一 Windows 用户或虚拟机隔离数据。备份时先从托盘退出，再复制整个目录。

| 路径                       | 内容                                            |
| -------------------------- | ----------------------------------------------- |
| `config/settings.json`     | 应用设置，包括首次引导、Git Bash 路径和日志保留 |
| `library/scripts/`         | 脚本副本、元数据和 DPAPI 密文                   |
| `state/ui_state.json`      | 最近选中和最近运行的脚本 ID                     |
| `history/runs.jsonl`       | 执行历史                                        |
| `logs/exec-YYYY-MM-DD.log` | 按执行开始日归档的日志                          |

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

安装包面向 Windows 10/11 x64。推荐 **NSIS 当前用户安装**（通常无需管理员），安装向导可选简体中文 / English，并可更改程序目录。Go 引擎随安装包提供。卸载会移除程序文件，`%APPDATA%\easy-command` 下的脚本、设置、历史和日志会保留。

安装包默认不做 Windows Authenticode 签名，首次运行时可能出现 SmartScreen 警告。确认安装包来自可信渠道后，若界面提供该选项，可点击「更多信息」再选择「仍要运行」。

`.bat` 使用 Windows 自带的 cmd.exe。`.sh` 需要单独安装 [Git for Windows](https://git-scm.com/install/windows)。引导会检测 Git Bash，也可手动指定安装目录下的 `bin\bash.exe`。密码密文由 Windows DPAPI 绑定当前用户。更换 Windows 用户、重装系统或迁移电脑后，如无法解密，请重新设置密码。

## 构建 Windows 安装包

| 目标         | 命令                            | 默认输出目录                                |
| ------------ | ------------------------------- | ------------------------------------------- |
| NSIS（推荐） | `pnpm tauri:build:nsis`         | `src-tauri/target/release/bundle/nsis/`     |
| MSI          | `pnpm tauri:build:msi`          | `src-tauri/target/release/bundle/msi/`      |
| 两者         | `pnpm tauri:build`              | 同上两个目录                                |
| 一键脚本     | `.\scripts\build-installer.ps1` | 同上（默认仅 NSIS；`-Bundle all` 两者都打） |

```powershell
pnpm install --frozen-lockfile
.\scripts\build-sidecar.ps1
pnpm tauri:build:nsis
```

或：

```powershell
.\scripts\build-installer.ps1
# .\scripts\build-installer.ps1 -Bundle msi
# .\scripts\build-installer.ps1 -Bundle all
```

依赖：Rust、Node/pnpm、[WebView2](https://developer.microsoft.com/microsoft-edge/webview2/)（安装包会带 bootstrapper）、WiX Toolset（仅打 MSI 时需要）。首次 NSIS 封装可能下载 Tauri 工具；若超时，恢复对官方源的访问后重跑。 [Tauri Windows 安装包](https://v2.tauri.app/distribute/windows-installer/)

版本号以 `src-tauri/Cargo.toml` 为准。发版时提升该版本，构建后将 `*-setup.exe`（及可选 MSI）放到分发渠道即可。

### 可选：应用内自动更新

默认 `createUpdaterArtifacts` 为 `false`，打包不生成 `.sig`，也不要求 `TAURI_SIGNING_PRIVATE_KEY`。若以后要启用应用内更新：

1. `pnpm tauri signer generate --write-keys <仓库外路径>`
2. 把公钥写入 `src-tauri/tauri.conf.json` 的 `plugins.updater.pubkey`
3. 将 `bundle.createUpdaterArtifacts` 设为 `true`，构建时注入私钥环境变量
4. 随 Release 发布 `latest.json` 与 `.sig`

### 发布前验收

- 在干净的 Windows 用户或虚拟机安装，选择安装目录，启动并完成引导，导入和运行脚本。
- 退出应用后卸载，检查 `%APPDATA%\easy-command` 中的脚本和历史仍可读取。
