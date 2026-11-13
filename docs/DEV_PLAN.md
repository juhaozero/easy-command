# Easy Command 分步开发计划

> 状态：框架设计初稿  
> 配套文档：[ARCHITECTURE.md](./ARCHITECTURE.md)

本计划将 MVP 拆成 8 步，从最小可运行骨架推进到可分发安装包。每步结束后打 tag / 合并，再开下一步。本轮仅文档，不写业务实现代码。

---

## 开发原则

- 严格遵守架构边界：前端不直连 Sidecar；业务与 JSON 存储在 Go；托盘 / 快捷键 / Updater 在 Rust。
- 每步可独立验收；不做跨步大爆炸合并。
- 不展开脚本执行、参数表单、历史记录的过早细节，直到对应步骤启动。

---

## 步骤总览

| 步    | 目标                   | 产出物                                                                               | 验收标准                                                                      |
| ----- | ---------------------- | ------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------- |
| **1** | 空壳可跑               | Vue + Vite + Tauri 2 工程；Hello 窗口；目录约定；docs 入库                           | `tauri dev` 出窗；无 Sidecar 也可显示占位 UI                                  |
| **2** | Sidecar 骨架 + JSONL   | Go module；hello / ping / shutdown；Rust spawn + 壳级 Job；崩溃重启；stdout 协议独占 | 双向 ping；杀引擎自动拉起；退出无僵尸；非协议 stdout 报 PROTOCOL_ERROR 不崩溃 |
| **3** | JSON 存储 + 脚本库 MVP | 导入复制、冲突策略、meta CRUD（含 `dangerous`）、库内编辑                            | 导入 bat 可见可改可删；`dangerous` 可读写；原子写不腐                         |
| **4** | 执行引擎 + 流式输出    | bat/cmd、sh/bash、`runId`/`script.cancel`、执行级 Job、危险确认入口                  | 实时输出；取消杀进程树；无 Bash 明确错误；dangerous 二次确认                  |
| **5** | 历史 + 日志保留 + 脱敏 | RunRecord、按日 log、保留策略                                                        | password 日志为 `***`；改保留策略后清理符合预期                               |
| **6** | DPAPI 密码参数         | secret.set、执行前解密、引导文案                                                     | 换用户无法解密；JSON 中无明文                                                 |
| **7** | 调度 + 快捷键 + 选中态 | Go sched；`hotkey.trigger` / `tray.runLast`；ui_state 仅 Go                          | 三种规则可触发；快捷键/托盘不经 Rust 缓存；失效错误码                         |
| **8** | 托盘 + 引导 + 打包更新 | 关窗托盘、菜单、onboarding、installer、updater 配置                                  | 可分发安装包；检查更新链路打通（可用 test endpoint）                          |

---

## 步骤详情

### 步骤 1：空壳可跑

**目标**：打通前端 + Tauri 开发环境，固定仓库目录约定。

**产出物**：

- `apps/web`：Vue 3 + TypeScript + Vite 占位页
- `src-tauri`：Tauri 2 最小工程与窗口
- `docs/ARCHITECTURE.md`、`docs/DEV_PLAN.md`（本轮已完成）
- 根 `README.md` 指向文档

**验收标准**：

- [ ] `pnpm` / `npm` 安装依赖后，`tauri dev` 能打开主窗口
- [ ] 窗口展示占位 UI（无需 Sidecar）
- [ ] 仓库目录与架构文档约定一致

---

### 步骤 2：Sidecar 骨架 + JSONL

**目标**：Rust 拉起 Go 引擎，完成握手与生命周期监管。

**产出物**：

- `sidecar/cmd/easy-command-engine`：`system.hello` / `ping` / `shutdown`；stdout 仅协议 JSONL
- `src-tauri/src/sidecar.rs`：spawn、stdin/stdout JSONL、崩溃重启、**壳级** Job Object
- 开发用 build 脚本将 engine 放到 sidecar 约定路径

**验收标准**：

- [ ] 开发模式 Rust ↔ Go 双向 ping 成功
- [ ] 手动结束 engine 进程后自动退避拉起
- [ ] 正常退出后无残留 engine 进程（无僵尸）
- [ ] 故意让 Sidecar 向 stdout 打印非协议内容时，Rust 识别为协议错误（`PROTOCOL_ERROR`），不崩溃

---

### 步骤 3：JSON 存储 + 脚本库 MVP

**目标**：落地 AppData 布局与脚本导入 / 元数据 CRUD / 库内编辑。

**产出物**：

- `store`：路径、原子写、index + per-script `meta.json`
- `script.import` / `get` / `list` / `updateMeta` / `updateContent` / `delete`
- `ScriptMeta.dangerous: boolean`（默认 `false`，可读写）
- 冲突策略：`conflictPolicy` = rename | overwrite | cancel（UI 选择）
- 前端脚本库列表与简单编辑页（含 dangerous 开关）

**验收标准**：

- [ ] 导入 `.bat` 后出现在列表，可改名、改正文、删除
- [ ] 同名冲突时 UI 可选三种策略且结果正确
- [ ] 写入中途杀进程，目标 JSON 不出现半截损坏（原子写生效）
- [ ] `dangerous` 可设置并持久化，列表/详情可读回

---

### 步骤 4：执行引擎 + 流式输出

**目标**：能跑脚本并实时看输出。

**产出物**：

- `exec`：`.bat` via `cmd.exe`；`.sh` via Git Bash；**执行级 Job**（cancel 用 `TerminateJobObject`）
- `bash.detect` / `bash.setPath`
- `script.run`（`res` = 启动成功 + `{ runId }`）/ `script.cancel`（payload `{ runId }`，受理即返回）
- `exec.*` 事件均带 `runId`；`exec.exit` 带 `exitCode` + `status`
- 前端执行台：实时日志区 + 退出码；manual + `dangerous` 二次确认
- Rust：hotkey/tray 路径预留 dangerous 二次确认钩子（完整托盘可在步骤 8 接线）

**验收标准**：

- [ ] 运行 bat 可实时看到 stdout/stderr；`script.run` 的 res 仅表示已启动并返回 `runId`；结束以 `exec.exit` 为准
- [ ] 已安装 Git Bash 时可跑 sh；未安装时返回明确 `BASH_NOT_FOUND`
- [ ] `script.cancel` 后 `exec.exit` 的 `status=cancelled`、`exitCode=null`；**cmd.exe 带出的子进程树无孤儿**
- [ ] `dangerous=true` 时 manual 触发会二次确认；取消确认则不发 `script.run`

---

### 步骤 5：历史 + 日志保留 + 脱敏

**目标**：执行可追溯，敏感参数不落明文日志。

**产出物**：

- `history`：`runs.jsonl` 追加与查询
- 按日 `logs/exec-YYYY-MM-DD.log`
- `logRetention`：`keep_forever` | `days`
- 按参数 `maskInLog` / password 强制脱敏

**验收标准**：

- [ ] 每次执行产生 RunRecord，历史页可查
- [ ] password 类参数在历史与日志中为 `***`
- [ ] 将保留改为 N 天后，超龄日志被清理；`keep_forever` 不删

---

### 步骤 6：DPAPI 密码参数

**目标**：密码入库加密，仅执行时瞬态解密。

**产出物**：

- `secret`：DPAPI wrap/unwrap
- `secret.set` / `secret.clear`
- meta 中 `ciphertext`；UI 仅显示是否已保存
- 首次引导 / 设置中的 DPAPI 说明文案

**验收标准**：

- [ ] `meta.json` 中无密码明文
- [ ] 同用户可解密并成功执行带密参脚本
- [ ] 说明文案明确：换用户 / 重装后需重新填写

---

### 步骤 7：调度 + 快捷键 + 选中态

**目标**：应用内定时与全局快捷键可用。

**产出物**：

- Go `sched`：cron / interval / daily；`pauseAll` / `resumeAll`；schedule 触发 dangerous 脚本时托盘通知「已执行危险脚本」
- Tauri global shortcut；Rust 只发 `hotkey.trigger` / `tray.runLast`，**不缓存**选中/最近 ID
- Go 维护 `ui_state.json`（`lastSelectedScriptId` / `lastRunScriptId`）；错误码 `NO_SELECTION` / `SELECTION_DISABLED` / `NO_LAST_RUN`
- 删除 / 禁用时 Go 清空选中并可选返回 `selectionInvalidated`

**验收标准**：

- [ ] 三种规则均可触发执行（`trigger=schedule`）
- [ ] 退出应用后定时不再触发
- [ ] 快捷键仅经 `hotkey.trigger`；无效选中返回错误码且 Rust 托盘提示、不执行
- [ ] 托盘「运行最近」仅经 `tray.runLast`；Rust 进程内无选中/最近 ID 缓存
- [ ] schedule 触发 `dangerous` 脚本不弹确认，但有托盘通知且历史可见

---

### 步骤 8：托盘 + 引导 + 打包更新

**目标**：可分发给他人安装，并具备更新与引导体验。

**产出物**：

- 关窗最小化托盘；首次关闭提示
- 托盘菜单：打开主界面、运行最近脚本、暂停定时、退出
- onboarding 向导（目录初始化、Git Bash、DPAPI 说明）
- NSIS / MSI 安装包；Tauri updater 配置（可先接 test endpoint）

**验收标准**：

- [ ] 关闭窗口进托盘；托盘菜单功能齐全
- [ ] 首次运行走完引导并可跳过 / 完成标记
- [ ] 打出可安装包；「检查更新」链路在测试 endpoint 下打通
- [ ] README 说明无代码签名时的 SmartScreen 提示

---

## 已选定默认（实现时沿用，变更需改架构文档）

| 项            | 默认                                      |
| ------------- | ----------------------------------------- |
| DPAPI         | Go Sidecar                                |
| 调度器        | Go Sidecar                                |
| 全局快捷键    | Tauri 注册；选中/最近 ID 仅 Go + ui_state |
| 取消进程树    | Go 执行级 Job + TerminateJobObject        |
| AppData 目录  | `%APPDATA%\easy-command`                  |
| 包 ID（暂定） | `com.easycommand.app`                     |
| 同名冲突      | UI 选择重命名 / 覆盖 / 取消               |
| 并行执行      | 允许；UI 先单会话                         |
| 退出策略      | shutdown 后最多等待 N 秒再强杀            |

---

## 待确认问题（影响架构）

以下问题会改变接口或行为，确认后应回写 [ARCHITECTURE.md](./ARCHITECTURE.md)。

1. **应用正式名与包 ID**：窗口标题、`%APPDATA%` 目录名、Tauri `identifier`、updater pubkey 归属（暂定 `easy-command` / `com.easycommand.app`）。
2. **Updater 发布渠道**：是否固定 GitHub Releases？私有仓库则需 token 方案。
3. **同名冲突默认策略**：是否需要「记住为默认」而不每次弹窗（当前每次由 UI 选择）。
4. **并发执行策略**：是否改为同一脚本单飞队列（当前 MVP 允许并行）。
5. **执行中退出应用**：等待当前任务结束、固定超时强杀、还是立即杀（当前：shutdown + 最多 N 秒）。
6. **.bat 编码**：默认系统 ANSI/GBK 还是强制 UTF-8（影响输出解码与编辑器）。
7. **历史体量**：JSONL 无限追加 vs 按条数 / 天数与日志策略对齐修剪。

---

## 本轮完成情况

- [x] `docs/ARCHITECTURE.md` 初稿
- [x] `docs/DEV_PLAN.md` 初稿
- [x] 根 `README.md` 指向文档
