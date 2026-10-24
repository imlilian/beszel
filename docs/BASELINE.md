# P0 基线记录

记录时间：2026-10-24
基线提交：`e2ed147a`（release 0.21.1）
分支：`custom-0.21.1`（**说明**：未另建 `feat/beszel-extensions` 分支。该分支已是 fork 的独立开发分支，且为个人项目、无协作方，另开分支只增加合并成本。若后续需要 PR 流程再切。）

## 1. 环境

| 项 | 值 |
|---|---|
| OS | Windows（win32），shell = Git Bash |
| Go | go1.27.3 windows/amd64 |
| Node | v22（`npx` 可用） |
| 前端依赖 | `cd internal/site && npm install` → 391 packages，0 vulnerabilities |

## 2. 首次跑测试前必须准备的三处占位（**关键**）

`go test ./...` 在干净检出上会直接 `[setup failed]`，原因是三个 `go:embed` 目标目录不存在，而这些目录都被 `.gitignore` 忽略。**这不是代码问题，是构建前置条件。**

| 报错 | 原因 | 处理方法 |
|---|---|---|
| `internal/site/embed.go:9: pattern all:dist: no matching files found` | 前端未构建 | `mkdir -p internal/site/dist && printf '<!doctype html>' > internal/site/dist/index.html`（等价于 `make build-hub-dev` 的第一步） |
| `agent/sensors_windows.go:43: pattern all:lhm/bin/Release/net48: no matching files found` | 未跑 `dotnet build`（Windows 专有） | `mkdir -p agent/lhm/bin/Release/net48 && touch agent/lhm/bin/Release/net48/.placeholder` |
| `agent/smart_windows.go:13: pattern smartmontools/smartctl.exe: no matching files found` | 未跑 `make fetch-smartctl-conditional` | `mkdir -p agent/smartmontools && touch agent/smartmontools/smartctl.exe` |

> 三个占位文件均在 `.gitignore` 覆盖范围内（`dist`、`agent/lhm/bin`、`*.exe`），不会污染提交。
> 正式发布前仍需 `make build`（会跑真实的 dotnet 构建与 smartctl 下载）。

## 3. 基线测试结果（在 `e2ed147a` 原始代码上测得）

用独立 git worktree 在 `e2ed147a` 上跑，排除本次改动影响：

```
FAIL  github.com/henrygd/beszel/agent
        --- FAIL: TestBuildFsStatRegistration
        --- FAIL: TestLegacyHubGetsSinglePayload (15.11s)
              legacy_cbor_hub_0.12.0 / legacy_json_hub_0.11.1 / unparsable_client_version
              → server_test.go:1031 "Condition never satisfied"
```

**结论：这两个失败是 Windows 环境固有的，与本次开发无关。**

- `TestLegacyHubGetsSinglePayload` 用 **Unix domain socket** 起 SSH server（`Starting SSH server addr=...\beszel-test.sock network=unix`），Windows 上不可用 → 超时 5s/子用例。
- `TestBuildFsStatRegistration` 同属文件系统相关的平台差异。

> **后续所有阶段的判定标准**：`./internal/...` 与 `./internal/hub/...` 必须全绿；`./agent` 允许上述 2 个用例失败，但**不得出现新的失败用例**。若需要严谨验证 agent 改动（P2、P10.4），用 `GOOS=linux go build ./...` 做编译校验 + 针对性单测。

## 4. 其余包基线

`./internal/...` 全部 `ok`（alerts 83s、hub 24s、hub/systems 27s、records 14s、users 12s 为耗时项）。前端 `npx tsc -b` 需先 `npm run sync` 生成 `src/locales/*.ts`（已被 gitignore）。

## 5. 常用命令（实际可用版）

```bash
# 全量 Go 测试（Windows 上 agent 包有 2 个已知失败）
go test -tags='testing no_ui' ./...

# 只测本次改动相关的包（推荐日常用）
go test -tags='testing no_ui' ./internal/... ./internal/hub/...

# agent 编译校验（跨平台，不改动本机）
GOOS=linux go build ./...

# 前端：先同步 i18n（新增文案后必跑），再类型检查 + 构建
cd internal/site && npm run sync && npx tsc -b && npm run build

# 开发态 Hub
make build-hub-dev
```

## 6. 工作区约定

- `.workbuddy/` 与 `docs/` 是本次新增的**未跟踪目录**，提交时按需 `git add`（`docs/` 应当纳入版本库，`.workbuddy/` 建议加进 `.gitignore`）。
- `internal/site/package-lock.json` 在 `npm install` 后会被本机 npm 版本改写（lockfileVersion 元数据 + `libc` 字段）。**每次提交前 `git checkout -- internal/site/package-lock.json`**，避免噪声。
