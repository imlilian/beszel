# 进度看板（跨会话交接单）

> **给接管的新 Agent**：先读本文件，再读 `docs/WORKPLAN-beszel-extensions.md` 的对应阶段，最后读 `docs/PRD-beszel-extensions.md` 的相关章节。
> 本文件是「已经发生了什么」的唯一权威来源，**避免重做已完成的阶段**。
>
> 最后更新：2026-10-24（P2 完成时）

## 1. 阶段状态

| 阶段 | 状态 | 提交 | 备注 |
|---|---|---|---|
| P0 基线 | ✅ 完成 | `5da4e42c` | 产出 `docs/BASELINE.md`、`docs/FOLLOWUPS.md` |
| P1 监控名称 | ✅ 完成 | `db5fc40c` | 45 文件（含 34 个 `.po`） |
| P2 900ms 计丢包 + 阈值 | ✅ 完成 | `28a05f0f` | Agent 归一化 + 前端阈值设置 |
| **P3 卡片上下行** | ⬜ **下一步** | — | 4 处小改，无迁移 |
| P4 监控卡片视图 | ⬜ 待开始 | — | 前端大头，拆 5 子步 |
| P5 计费数据模型 | ⬜ 待开始 | — | 2 个新集合 |
| P6 计费前端 | ⬜ | — | — |
| P7 到期提醒 | ⬜ | — | — |
| P8 流量后端 | ⬜ | — | — |
| P9 流量前端 | ⬜ | — | — |
| P10 可选 | ⬜ | — | 流量提醒 / 历史 / 连接数 |

分支：`custom-0.21.1`（**未另建 feature 分支**，见 §4 偏差 1）
基线提交：`e2ed147a`（release 0.21.1）
工作区：干净（`git status` 无输出）

## 2. 每个阶段开始前必做

```bash
cd D:/work/vscode/go/beszel
git log --oneline -3          # 确认在最新提交上
git status --short            # 应无输出
```

三处 `go:embed` 占位目录若丢失会导致 `[setup failed]`，重建方法见 `docs/BASELINE.md` §2。

## 3. 已落地的对外接口（后续阶段可直接复用，不要重写）

| 符号 | 位置 | 用途 |
|---|---|---|
| `getMonitorDisplayName(m)` | `site/src/lib/monitor-compare.ts` | 名称优先、空则回退 `getMonitorTarget`。P4 卡片标题直接用它 |
| `getMonitorThresholds(settings)` | `site/src/lib/monitor-thresholds.ts` | 从 `$userSettings` 取出并清洗阈值（字符串强转、非法回落、保证 `crit > warn`） |
| `getMonitorLevel(resUs, t)` | 同上 | 返回 `"good"｜"warn"｜"crit"｜"loss"` |
| `monitorLevelBg[level]` | 同上 | 圆点 / 色块背景色类 |
| `monitorLevelText[level]` | 同上 | 文本色类（**目前未被使用**，P4 会用到） |
| `DEFAULT_MONITOR_THRESHOLDS` | 同上 | `{ warnMs: 150, critMs: 400 }` |
| `lossThresholdUs()` / `classifyProbe()` | `agent/network_monitor_thresholds.go` | Agent 侧丢包归一化 |
| `network_monitors.name` | 迁移 `1792847604` | 已在 `NETWORK_MONITOR_FIELDS`、`copyMonitorToNewRecord`、表单、批量 CSV 中打通 |

**订阅设置的正确姿势**（避免整表重渲染）：
```ts
const s = useStore($userSettings, { keys: ["monitorWarnMs", "monitorCritMs"] })
```

## 4. 与计划的偏差（计划文档尚未完全同步的地方，以本节为准）

1. **未新建 `feat/beszel-extensions` 分支**，直接在当前 fork 分支 `custom-0.21.1` 上按阶段提交。个人项目、无协作方，另开分支只增加合并成本。
2. **展示阈值用扁平键** `monitorWarnMs` / `monitorCritMs`，不是计划里的嵌套对象 `monitorThresholds: {warnMs, critMs}`。原因：设置页用 `FormData` + `Object.fromEntries` 提交，扁平键与既有的 `colorWarn` / `colorCrit` 一致，少一层序列化。PRD §6.3 已同步。
3. **颜色用项目既有的 `green-500` / `yellow-500` / `red-500`**，不是 PRD 早期写的 `emerald`。与 `certExpiryDotColors`、`SYSTEM_STATUS_COLORS` 保持同一套绿。PRD 已同步。
4. **阈值设置 UI 已实现**（计划 P2 只要求"读用户设置"，未明确提出 UI）：`routes/settings/general.tsx` 的「Monitor thresholds」小节。
5. **批量 CSV 增加可选第 6 列 `name`**（`target,protocol,port,interval,server,name`），5 列旧格式仍兼容；含逗号的名称会解析错位，已记入 `FOLLOWUPS.md`。
6. **`getMonitorLevel` 把 `null` 和负数都判为 `loss`**。注意区分：`empty`（无探测样本）由 P4 的块聚合自行判断，**不能**复用 `getMonitorLevel` 的这个分支，否则 5 分钟探测的监控会显示大面积丢包。

## 5. 验证命令（Windows 实测可用）

```bash
# Go：internal 必须全绿
go test -tags='testing no_ui' ./internal/...

# Go：agent 包有 2 个 Windows 固有失败（非回归，见 BASELINE.md §3）
go test -tags='testing no_ui' ./agent/ -run "TestMonitor|TestLoss|TestClassify|TestSlow"

# 前端
cd internal/site && npm run sync && npx tsc -b && npm run build

# 前端单测（本机无全局 bun）
cd internal/site && npx --yes bun test src/lib/

# 提交前必做（npm install 会改写 lockfile）
git checkout -- internal/site/package-lock.json
```

## 6. 每个阶段的收尾清单

```
- [ ] go build ./... 通过
- [ ] go test ./internal/... 全绿
- [ ] ./agent 无新增失败用例
- [ ] 前端 npm run sync + npx tsc -b + npm run build 通过（涉前端时）
- [ ] 新文案已补 zh-CN 翻译并重新 sync
- [ ] git status 无 package-lock.json 改动
- [ ] 提交信息以 stage-N: 开头
- [ ] 更新本文件（docs/PROGRESS.md）的阶段状态与偏差
- [ ] 新发现但本阶段不修的问题记入 docs/FOLLOWUPS.md
```
