# 进度看板（跨会话交接单）

> **给接管的新 Agent**：先读本文件，再读 `docs/WORKPLAN-beszel-extensions.md` 的对应阶段，最后读 `docs/PRD-beszel-extensions.md` 的相关章节。
> 本文件是「已经发生了什么」的唯一权威来源，**避免重做已完成的阶段**。
>
> 最后更新：2026-10-24（P4 完成时）

## 1. 阶段状态

| 阶段 | 状态 | 提交 | 备注 |
|---|---|---|---|
| P0 基线 | ✅ 完成 | `5da4e42c` | 产出 `docs/BASELINE.md`、`docs/FOLLOWUPS.md` |
| P1 监控名称 | ✅ 完成 | `db5fc40c` | 45 文件（含 34 个 `.po`） |
| P2 900ms 计丢包 + 阈值 | ✅ 完成 | `28a05f0f` | Agent 归一化 + 前端阈值设置 |
| P3 卡片上下行 | ✅ 完成 | `80197c5e` | `Info` 加 `bu/bd`（cbor 28/29），卡片 ↑↓ 分开显示 |
| P4 监控卡片视图 | ✅ 完成 | `f207b34f`→`d954fdd8`（5 提交） | 表格/卡片切换 + 20 色块 + 卡片墙 |
| P5 计费数据模型 | ⬜ **下一步** | — | 2 个新集合 |
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
| `system.Info.BandwidthUp/Down`（`bu`/`bd`） | `internal/entities/system/system.go` | 分向上/下行 bytes/s；旧 Agent 不上报，零值即"未知" |
| `getBandwidthParts(info)` | `site/src/components/systems-table/systems-table-columns.tsx` | 返回 `{up, down, combined}`；`combined` 为 true 时用合并值 `bb` 单值渲染 |
| `getBlockSpec` / `buildQualityBlocks` / `getBlockLevel` | `site/src/lib/monitor-blocks.ts` | 纯函数，按探测次数聚合成 20 个色块；`total === 0` 判 `empty` 不判丢包 |
| `useMonitorCardStats({monitors, thresholds, enabled})` | `site/src/lib/use-monitor-cards.ts` | 每 type 一次 `getFullList` + 1 个 realtime 订阅，返回 `Map<monitorId, QualityBlock[]>`（新→旧） |
| `QualityBlocks` / `MonitorCardGrid` / `getMonitorGridClass` | `site/src/components/network-monitors-table/` | 色块条（含部分丢包红边）与系统卡片墙；网格类必须照抄，别改断点 |

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
7. **P3 的 `bu/bd` 用 `omitzero`**（PRD §7.1 原写 `omitempty`），与 `Info` 里 `Battery` / `SystemdLogs` 一致；JSON 与 CBOR 行为统一，零值不上报。cbor 28/29 已核实空闲（`Info` 占用 0–27，其中 19 是 LoadAvg，15–17 为废弃不复用区）。
8. **P3 的 `getBandwidthParts` 放在 `systems-table-columns.tsx` 顶部**（计划允许放 `lib/utils.ts`）：只服务 net 列且需要 `SystemInfo` 类型。**未加**计划里标为可选的 `↕` 合并图标，旧 Agent 视觉与 v0.21.1 完全一致。
9. **P3 的 grid 两行渲染直接用已有的 `viewMode` 参数**（`SystemsTableColumns(viewMode)` 在 P4 之前就已存在），无需新增状态；`accessorFn` 仍取 `info.bb`，所以 table/grid 两种视图排序结果一致。
10. **P4：`alignNow` 用 `Math.ceil`，不是计划里的 `floor+1`**。计划同时要求"对齐后不抖动"和"幂等"（单测第 8 条），而 `floor+1` 在对齐点上会再加一个块（不幂等），右端色块会空一拍。`ceil(now / blockMs) * blockMs` 两条都满足，归一化块序号也不变。
11. **P4：卡片右上角菜单不含 `AddMonitorDialog` 触发器**。它的触发器是「Add Monitor + 下拉」的文字按钮，放进 `lg:grid-cols-3` 的卡片头和系统名抢空间。全局页工具栏与单系统页已有同一入口（都能选系统），功能不缺失。卡内菜单保留「全部暂停 / 全部启用 / 系统详情」。
12. **P4：`useMonitorCardStats` 多一个可选 `enabled`**（计划接口没写）。表格视图下不拉数据、不订阅，省掉一个全局 `network_monitor_stats` 实时订阅。
13. **P4：卡片"无数据"时丢包显示 `—`**（PRD §6.4.4 口径），不是计划骨架里的无条件 `loss1h.toFixed(1)`。判据用 `resAvg1h === 0 && loss1h === 0`（`network_monitors` 记录里没有 `sampleCount` 字段，PRD 那句写的是理想字段）。
14. **P4 的人工验收矩阵尚未在真实实例上跑过**（0/1/3/5/8 系统、interval 60s/120s/300s、全丢、窄屏、深色、RTL、200 监控下的请求数）。已通过的自动化部分：`tsc -b`、`vite build`、`bun test src/lib/`（44 pass，含色块聚合 11 条）、产物 CSS 已生成 `@[340px]` / `@[560px]` 容器查询规则。要做人工验收需 `make build-hub-dev` 并接入真实 Agent 数据。

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
