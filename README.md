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

当前实现对应开发计划 **步骤 3：JSON 存储 + 脚本库 MVP**。主窗口提供脚本列表、导入、详情、编辑和删除。

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

## 仓库目录

- `apps/web/` 放置 Vue、TypeScript 和 Vite 前端。
- `src-tauri/` 放置 Tauri 桌面壳。
- `sidecar/` 放置 Go 引擎。
- `docs/` 放置架构与开发计划。
- `scripts/` 用于开发辅助脚本。

## 说明

- 仅支持 Windows；`.sh` 依赖 Git Bash。
- 密码类参数使用 Windows DPAPI 加密，绑定当前用户账户。
- 分发使用 Tauri Updater；MVP 不强制代码签名，安装时可能出现 SmartScreen 提示。
