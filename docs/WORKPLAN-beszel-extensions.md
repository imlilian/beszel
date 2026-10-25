# Beszel 个人增强版 分阶段实施工作计划

- 版本：v1.1
- 配套文档：`docs/PRD-beszel-extensions.md`（**先读 PRD，再读本文**）
- **进度看板：`docs/PROGRESS.md`（先看它，确认哪些阶段已完成、有哪些与本文的偏差）**
- 基线：Beszel v0.21.1（commit `e2ed147a`）
- 目标读者：后续执行 Agent（每个阶段可以独立开一次会话，按阶段边界提交）

> **v1.1 修订记录**（2026-10-24）
> 1. **P4 整节重写**：取消 `sparkline.tsx`，改为竖状色块（`monitor-blocks.ts` + `quality-blocks.tsx`）。
> 2. P4 从 4 个子步骤扩到 5 个，新增「先抽离 `NetworkMonitorSheet`」的前置步骤。
> 3. **P10.3「per-monitor 阈值」取消**（Q6 已确认全局）。
> 4. 新增 **P10.4 TCP/UDP 连接数**（仅 Linux，可选）。
> 5. 附录 A/C 同步更新，新增「empty ≠ loss」等 4 条色块相关坑。
>
> ⚠️ **P0–P3 的改动清单未变**，可直接开工。

---

## 0. 给执行 Agent 的总守则（每个阶段开始前必读）

### 0.1 铁律

1. **一个阶段 = 一个可编译、可回滚的提交**。阶段结束时必须满足：
   - `make test` 全绿
   - `cd internal/site && npm run build`（或 `bun run build`）通过（涉及前端的阶段）
   - `git status` 干净，`git log` 每条提交信息以 `stage-N:` 开头
2. **禁止跨阶段顺手改别的功能**。发现的问题写进 `docs/FOLLOWUPS.md`，不要在本阶段修。
3. **禁止改 `beszel.MinVersionNetworkMonitors`**，除非阶段明确要求。
4. **禁止把配置写进 `systems` 集合**（`systems` 是实时订阅对象，见 PRD §0 第 3 条）。
5. **所有新增界面文案必须走 Lingui**：组件内用 `<Trans>` / `t\`\``，然后跑 `npm run sync` 更新 `locales`。（注意范围：Lingui 只管前端界面。**邮件 / Webhook 的通知正文由 Go 在投递前拼装，走 `alerts_*.go` 的既有英文模板**，不要在后端引入 i18n，原因见 P7「消息文案」。）
6. **颜色必须用 Tailwind 语义类**（同时给 `dark:` 变体），禁止硬编码 hex。
7. **任何写数据库的逻辑必须能重复执行**（幂等），尤其是 Cron。
8. **不确定的数据库字段名先看 `internal/migrations/0_collections_snapshot_0_20_0.go`**，不要凭想象。

### 0.2 常用命令

```bash
# 全量测试（注意 tags）
make test                      # = go test -tags='testing no_ui' ./...

# 只测某个包
go test -tags='testing no_ui' ./agent/... -run Monitor -v
go test -tags='testing no_ui' ./internal/hub/systems/... -v

# 前端类型检查 + 构建
cd internal/site && npm run build

# 只同步 i18n（不构建）
cd internal/site && npm run sync

# 本地跑 Hub（需先有 dist/index.html 占位）
make build-hub-dev && ./build/beszel-dev_$(go env GOOS)_$(go env GOARCH) serve --http 0.0.0.0:8090

# 本地跑 Agent
KEY="..." HUB_URL="http://localhost:8090" make dev-agent

# 生成新迁移文件名的时间戳（必须大于 1792309602）
date +%s
```

### 0.3 阶段总览与依赖

```
P0 基线
 └─ P1 监控名称（跨栈最小闭环，练手）
     └─ P2 900ms 计丢包 + 阈值工具
         └─ P3 卡片上下行（Info 扩展）
             └─ P4 监控卡片视图
                 
P5 计费数据模型 + 后端（与 P1-P4 无依赖，可并行）
 └─ P6 计费前端
     └─ P7 到期提醒 Cron
         └─ P8 流量后端
             └─ P9 流量前端
                 └─ P10（可选）流量提醒 / 历史 / 连接数
```

> 建议顺序：**P0 → P1 → P2 → P3 → P4 → P5 → P6 → P7 → P8 → P9**。
> P1–P4 是“网络监控与卡片”一条线，P5–P9 是“计费”一条线。两条线唯一的交汇点是 `systems-table-columns.tsx`（卡片渲染），因此**先做完 P4 再做 P6**，避免同一文件反复改。

### 0.4 阶段完成检查清单（每个阶段复制粘贴到提交说明里）

```
- [ ] 代码编译通过（go build ./...）
- [ ] make test 全绿
- [ ] 前端 npm run build 通过（如涉及）
- [ ] 新增文案已跑 npm run sync
- [ ] 新增字段已加入对应的 fields 常量（前端查询白名单）
- [ ] 新增集合已在 collections.go 配好权限
- [ ] 手动验证了本阶段的验收项
- [ ] 未修改无关文件
```

---

## 阶段 P0：基线与环境（半天）

### 目标

建立可重复的验证基线，确认后续改动有据可依。

### 步骤

1. `git status` 确认工作区干净；`git checkout -b feat/beszel-extensions`。
2. 跑基线测试并记录结果：
   ```bash
   make test 2>&1 | tail -30
   cd internal/site && npm ci && npm run build
   ```
   记录：哪些测试原本就慢/失败（若原本就失败，写进 `docs/BASELINE.md`，后续基于此判断回归）。
3. 建 `docs/FOLLOWUPS.md`（空文件，用于记“发现但本阶段不修”的问题）。
4. 确认本地能起 Hub + Agent（能用 Docker 或本机），能连上并看到系统卡片。
5. 准备一个**可控延迟测试目标**用于 P2：
   - 最简：一台机器 `tc qdisc add dev lo root netem delay 1200ms`（Linux）
   - 或起一个故意 sleep 的 TCP 服务（`ncat -l 9000 --sh-exec 'sleep 2'`）

### 交付物

- `docs/BASELINE.md`（基线测试结果）
- `docs/FOLLOWUPS.md`
- 分支 `feat/beszel-extensions`

### 验收

- `make test` 结果与 `docs/BASELINE.md` 一致
- 本地 Hub 能看到至少一台系统

---

## 阶段 P1：网络监控「名称」字段（跨栈最小闭环）

> 为什么第一个做：改动横跨**迁移 → Hub → 前端类型 → 表单 → 展示**全链路，但每处都很小，能最快暴露“我漏了哪个白名单”的问题，为后续阶段建立节奏。

### 目标

`network_monitors` 支持可选 `name`；全站展示优先用名称。

### 依赖

P0

### 改动清单（逐项必须有）

#### 1. 迁移 `internal/migrations/<ts>_network_monitor_name.go`

```go
package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("network_monitors")
		if err != nil {
			return err
		}
		collection.Fields.Add(&core.TextField{
			Id:   "nm_name",
			Name: "name",
			Max:  100,
		})
		return app.Save(collection)
	}, func(app core.App) error { // 回滚
		collection, err := app.FindCollectionByNameOrId("network_monitors")
		if err != nil {
			return err
		}
		collection.Fields.RemoveById("nm_name")
		return app.Save(collection)
	})
}
```

> ⚠️ 字段 `Id` 必须是 6 位以上唯一标识（参考 `nm_server`），不要用 `name`。

#### 2. Hub：`internal/hub/network_monitors.go`

`copyMonitorToNewRecord` 的字段切片加入 `"name"`：

```go
fields := []string{"system", "target", "name", "protocol", "port", "server", "interval", "enabled", "skipTlsVerify"}
```

**不要**改 `monitorConfigFromRecord`、**不要**改 `generateMonitorID`。

#### 3. 前端类型 `internal/site/src/types.d.ts`

```ts
export interface NetworkMonitorRecord {
  ...
  /** 显示用名称，空则回退到 target */
  name?: string
}
```

#### 4. 前端查询白名单 `lib/use-network-monitors.ts`

```ts
const NETWORK_MONITOR_FIELDS =
  "id,system,name,target,protocol,port,server,interval,skipTlsVerify,res,resMax1h,resMin1h,resAvg1h,loss1h,enabled,certInfo,updated"
```

以及 `COMPARE_MONITOR_FIELDS` 加 `name`（对比图例也要用名称）。

#### 5. 显示函数 `lib/monitor-compare.ts`

```ts
/** 展示名：优先用户填写的名称，否则回退到协议化的 target。 */
export function getMonitorDisplayName(monitor: Pick<NetworkMonitorRecord, "name" | "target" | "protocol" | "port">) {
  const name = monitor.name?.trim()
  return name ? name : getMonitorTarget(monitor)
}
```

> 放在 `monitor-compare.ts` 是因为该文件“不依赖 UI 与 store，可用 bun 单测”。

#### 6. 使用点替换（全部）

| 文件 | 位置 | 改法 |
|---|---|---|
| `network-monitors-columns.tsx` | target 列 cell | 主行 `getMonitorDisplayName`，若名称非空再显示一行 `text-[11px] text-muted-foreground` 的 `getMonitorTarget` |
| `network-monitors-table.tsx` | `globalFilterFn` | 搜索串加入 `monitor.name ?? ""` |
| `network-monitors-table.tsx` | `NetworkMonitorSheetContent` 的 `monitorLabel` | 改为 `getMonitorDisplayName(monitor)`，并在 SheetDescription 里额外显示 target |
| `monitors-charts.tsx` | `MonitorChart` 的 `getLabel = getMonitorTarget` 默认值 | 改为 `getMonitorDisplayName` |
| `monitor-compare.ts` | `getSeriesLabel` / 标签去重逻辑 | 用 `getMonitorDisplayName` 作为 `target` 参数 |

> ⚠️ `getMonitorIdentityKey` **不能**改成用名称 —— 它用于跨系统比对“同一个探测目标”，必须用 `protocol+target+port+server`。

#### 7. 添加/编辑对话框 `components/network-monitors-table/monitor-dialog.tsx`

- `MonitorValues` 加 `name: string`
- `NormalizedMonitorValuesSchema` 加 `name: z.string().trim().max(100).optional()`（空字符串 → `""`）
- 表单在 `target` 之前插入「名称（可选）」输入框，`placeholder={t\`e.g. Shanghai Telecom\`}`
- `buildMonitorPayload` 带 `name: values.name.trim()`，**空字符串要显式传 `""`**（便于清空）
- 批量导入：若 CSV 有 `name` 列则读取，无则 `""`；**不要破坏现有列顺序解析**

### 测试

1. `go test -tags='testing no_ui' ./internal/hub/... -run Monitor`
2. 单测：`internal/site/src/lib/monitor-compare.test.ts` 加用例
   ```ts
   expect(getMonitorDisplayName({name:" 上海电信 ", target:"1.2.3.4", protocol:"tcp", port:443})).toBe("上海电信")
   expect(getMonitorDisplayName({name:"  ", target:"1.2.3.4", protocol:"tcp", port:443})).toBe("1.2.3.4:443")
   ```
3. 手动：新增监控填名称 → 列表/Sheet/图例显示名称；清空 → 回退 target。
4. **回归关键**：改已存在监控的名称 → 观察 `network_monitor_stats` 是否连续（不应断点）。原理：ID 不含 name。

### 验收

PRD §11「监控名称」全部打勾。

### 风险

- 漏改 `NETWORK_MONITOR_FIELDS` → 前端永远拿不到 `name`（**最容易犯的错，务必检查**）。
- 漏改 `copyMonitorToNewRecord` → 用户改了 `interval` 导致记录重建，名称静默丢失（**第二容易犯的错**）。

### 提交

```
stage-1: add optional name field to network monitors
```

---

## 阶段 P2：900ms 以上视为丢包 + 阈值工具

### 目标

Agent 侧把 `>= 900ms` 的探测样本归一化为丢包；前端抽出统一的颜色阈值工具（为 P4 卡片铺路）。

### 依赖

P1

### 改动清单

#### 1. Agent 阈值常量 `agent/network_monitor_thresholds.go`（新建）

```go
package agent

import (
	"os"
	"strconv"
)

// defaultLossThresholdUs: 响应时间达到该值即视为丢包（与 Komari 对齐）。
const defaultLossThresholdUs int64 = 900_000 // 900ms

// lossThresholdUs 读取 MONITOR_LOSS_THRESHOLD_MS（毫秒），<= 0 表示关闭。
func lossThresholdUs() int64 {
	if v, ok := os.LookupEnv("MONITOR_LOSS_THRESHOLD_MS"); ok {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			if ms <= 0 {
				return 0
			}
			return ms * 1000
		}
	}
	return defaultLossThresholdUs
}
```

> 用 `os.LookupEnv` 或项目内 `agent/utils` 的 `GetEnv`（保持与 `NICS` 一致，优先用 `utils.GetEnv`）。

#### 2. 归一化 `agent/network_monitor_task.go`

在 `runProbe` 的 `responseUs, err := probe(...)` 之后：

```go
responseUs, err := probe(task.ctx, task.config)
lossThreshold := lossThresholdUs()
var logFailure, logSlow bool
...
if err != nil {
    responseUs = -1
    logFailure = true
    ...
} else if lossThreshold > 0 && responseUs >= lossThreshold {
    // 慢响应按丢包处理，保证 loss/avg/历史/告警口径一致（见 PRD §6.2）
    responseUs = -1
    logSlow = true
}
```

- 日志限频逻辑复用 `task.lastFailureLog`（同一套限频即可，不必再开一个字段）。
- 日志文案区分：`slog.Warn("monitor slow (treated as loss)", "res_us", raw, "threshold_us", lossThreshold, ...)`。
- ⚠️ **保留原始值用于日志**，别把 `responseUs` 覆盖掉后再打日志。

#### 3. 前端阈值工具 `internal/site/src/lib/monitor-thresholds.ts`（新建）

```ts
export type MonitorLevel = "good" | "warn" | "crit" | "loss" | "empty"

export const DEFAULT_MONITOR_THRESHOLDS = { warnMs: 150, critMs: 400 }

export function getMonitorLevel(resUs: number | null | undefined, t = DEFAULT_MONITOR_THRESHOLDS): MonitorLevel {
  if (resUs == null || resUs < 0) return "loss"
  if (resUs <= t.warnMs * 1000) return "good"
  if (resUs <= t.critMs * 1000) return "warn"
  return "crit"
}

/** 设置值可能是字符串（FormData 提交）、空、0 或大小颠倒，统一在这里收拾干净 */
export function getMonitorThresholds(settings?: Partial<UserSettings> | null): MonitorThresholds

/** 背景色 / 文本色两张表，表格与 P4 色块共用，避免两处配色漂移 */
export const monitorLevelBg: Record<MonitorLevel, string>
export const monitorLevelText: Record<MonitorLevel, string>
```

#### 4. 用户设置类型 `types.d.ts::UserSettings`（扁平键，与 `colorWarn`/`colorCrit` 一致）

```ts
/** 展示用延迟阈值（毫秒），仅影响颜色不影响数据 */
monitorWarnMs?: number
monitorCritMs?: number
```

#### 5. 替换硬编码阈值 `network-monitors-columns.tsx`

- 删除 `responseTimeThresholds`（按协议硬编码），改为 `getMonitorThresholds(useStore($userSettings))`。
- 单元格圆点颜色统一走 `monitorLevelBg[getMonitorLevel(...)]`；`muted` 状态仍单独处理。

#### 6. 设置入口 `routes/settings/general.tsx`

在「Warning thresholds」之后新增「Monitor thresholds」小节（两个 `type="number"` 输入，`name="monitorWarnMs"` / `monitorCritMs`）。
**注意**：该页面用 `FormData` + `Object.fromEntries` 取所有字段，值都是**字符串**，提交前必须用 `getMonitorThresholds(data)` 归一化后再写回，否则会把 `"150"` / `""` 存进 JSON。

### 测试

1. `agent/network_monitor_thresholds_test.go`（新建）：`lossThresholdUs` 的默认/覆盖/0/负数/非法值；`classifyProbe` 阈值边界（899_999 / 900_000）；归一化后 `PacketLoss`、`SuccessCount`、`ResponseSum`、`AvgResponse` 同步变化。
2. `internal/site/src/lib/monitor-thresholds.test.ts`（新建，`bun test`）：默认值、字符串强转、非法值回落、`crit > warn` 约束、150/400ms 边界。
3. 回归：现有 `network_monitor_history_test.go` 用例（`responseUs: 1000` = 1ms）**不应受影响**。
4. 手动：`tc netem delay 1200ms` 目标 → UI 丢包 100%，`loss1h` 同步；`delay 500ms` → 红色但不丢包。
5. 手动：设置页把 warn 改成 300ms，表格里 200ms 的条目应从绿变黄。

### 验收

PRD §11「900ms 计丢包」全部打勾。

### 风险

- **别在 `addResponse` 里加阈值判断**：那里拿不到配置，且会被 `addAggregate` 二次合并绕过。必须在 `runProbe` 归一化。
- 改完要检查丢包告警是否被“误伤”（`alerts_network_monitors.go` 的阈值是百分比，不受影响，但要跑一遍现有告警测试）。

### 提交

```
stage-2: treat >=900ms probes as packet loss; make latency thresholds configurable
```

---

## 阶段 P3：系统卡片上下行分开显示

### 目标

`system.Info` 增加上行/下行速率字段，卡片同时显示 ↑ ↓。

### 依赖

P2（因为卡片渲染在 P4 还会再改一次，先做这个让渲染逻辑稳定）

### 改动清单

#### 1. `internal/entities/system/system.go::Info`

```go
BandwidthBytes uint64             `json:"bb" cbor:"18,keyasint"`
...
BandwidthUp    uint64             `json:"bu,omitzero" cbor:"28,keyasint,omitzero"` // bytes/s, sent
BandwidthDown  uint64             `json:"bd,omitzero" cbor:"29,keyasint,omitzero"` // bytes/s, recv
```

> cbor key 28/29 是**下一个可用编号**（0–27 已占用，15–17 是废弃不复用）。

#### 2. `agent/system.go`

```go
a.systemInfo.BandwidthBytes = systemStats.Bandwidth[0] + systemStats.Bandwidth[1]
a.systemInfo.BandwidthUp    = systemStats.Bandwidth[0]
a.systemInfo.BandwidthDown  = systemStats.Bandwidth[1]
```

#### 3. `internal/site/src/types.d.ts::SystemInfo`

```ts
/** 上行速率 bytes/s（旧 Agent 无此字段） */
bu?: number
/** 下行速率 bytes/s（旧 Agent 无此字段） */
bd?: number
```

#### 4. 表格/卡片网络列 `systems-table-columns.tsx`（**精确锚点：`id: "net"`，约 256–277 行**）

现状：

```tsx
{
	accessorFn: ({ info, status }) => (status !== SystemStatus.Up ? undefined : info.bb),
	id: "net",
	cell(info) {
		const val = info.getValue() as number | undefined
		if (val === undefined) return null
		const userSettings = useStore($userSettings, { keys: ["unitNet"] })
		const { value, unit } = formatBytes(val, true, userSettings.unitNet, false)
		return <span className="tabular-nums whitespace-nowrap">{decimalString(value, value >= 100 ? 1 : 2)} {unit}</span>
	},
}
```

改动：

1. **`accessorFn` 不动**，继续用 `info.bb`（总吞吐）。理由：排序语义保持"总带宽"稳定；且新 Agent 的 `bb` 本来就等于 `bu + bd`，两种 Agent 排序结果一致。
2. 新增小工具（放在该文件顶部或 `lib/utils.ts`）：

```ts
/** 旧 Agent 只上报合并值 bb；新 Agent 额外上报 bu/bd。 */
export function getBandwidthParts(info: SystemInfo) {
	if (info.bu || info.bd) return { up: info.bu ?? 0, down: info.bd ?? 0, combined: false as const }
	return { up: info.bb ?? 0, down: null, combined: true as const }
}
```

3. cell 渲染：
   - `combined === false`：`viewMode === "table"` 时单行紧凑 `↑1.2 M/s ↓8.4 M/s`；grid 时两行（`↑` 一行、`↓` 一行），因为卡片列宽窄。
   - `combined === true`（旧 Agent）：保持现有单值渲染，前面加一个 `↕` 小图标暗示"未分向"，不加也可以。
   - 单位各自复用 `formatBytes(v, true, userSettings.unitNet, false)` 即可。**注意 `formatBytes` 会按数值自动选量级**（没有"强制指定单位"的参数），所以上行下行可能显示成 `↑1.2 MB/s` / `↓800 KB/s`。这是可接受的（Nezha/Komari 也是各自换算）；**不要**为了凑成同一单位去手写换算表或改 `formatBytes` 签名。

#### 5. `internal/alerts/alerts_system.go`

`Bandwidth` 告警仍用 `BandwidthBytes`（合并值），**不改**。但需确认 `bu/bd` 不会影响它（不同字段，无影响）。

### 测试

1. `internal/hub/systems/system_test.go` 加 Info round-trip 测试。
2. 手动：新 Agent 卡片出现 ↑↓；把 Agent 降级到旧版（或手工构造 `info` 只有 `bb`）→ 回退显示合并值。

### 验收

PRD §11「上下行」两条。

### 提交

```
stage-3: expose up/down bandwidth separately in system info
```

---

## 阶段 P4：网络监控卡片视图（竖状色块）

> 这是三个需求里**前端工作量最大**的一个。拆成 5 个子步骤，每子步都可编译。
>
> **v1.1 修订**：取消迷你折线（原 `sparkline.tsx`），改为 Komari 风格竖状色块。折线只在 Sheet 里保留（已有组件，不改）。

### 目标

全局/单系统监控页支持 `表格 ⇄ 卡片`；卡片按系统聚合，卡内最多 3 个监控块，每块含「名称 + 协议 / 平均延迟 / 丢包率 / 20 个竖状色块」。

### 依赖

P1（名称，用于块标题）、P2（阈值工具，用于配色）

### 先看三个规格（照抄 PRD §6.4，不要自行发明）

| 项 | 值 |
|---|---|
| 色块数量 | 固定 20，宽度 `flex-1` 均分 |
| 块时长 | interval ≤120s → 2min（窗口 40min，用 `type=1m`）；interval >120s → 10min（窗口 200min，用 `type=10m`） |
| 卡片数字 | 延迟 `resAvg1h`、丢包 `loss1h`（**平均值，不是最后一次探测**） |
| 最多显示 | 每个系统卡片最多 3 个监控块，超出折叠为 `+N more` |

### 子步骤

#### P4.1 纯函数：`internal/site/src/lib/monitor-blocks.ts`（新建，先做，可单测）

```ts
import type { NetworkMonitorRecord, RawMonitorStatsRecord } from "@/types"
import { getMonitorLevel, type MonitorLevel, type MonitorThresholds } from "./monitor-thresholds"

export const BLOCK_COUNT = 20

export interface BlockSpec {
  blockMs: number
  count: number
  type: "1m" | "10m"
}

/**
 * 按探测间隔分级：
 * - interval <= 120s：20 块 × 2 分钟 = 40 分钟窗口，用 1m 记录（1m 只保留 1 小时，40min 安全）
 * - interval >  120s：20 块 × 10 分钟 = 200 分钟窗口，用 10m 记录（保留 12 小时）
 * 长间隔若仍用 2 分钟块，40 分钟内只有 8 个样本，色条会有 12 块灰。
 */
export function getBlockSpec(monitor: Pick<NetworkMonitorRecord, "interval">): BlockSpec {
  return monitor.interval > 120
    ? { blockMs: 10 * 60_000, count: BLOCK_COUNT, type: "10m" }
    : { blockMs: 2 * 60_000, count: BLOCK_COUNT, type: "1m" }
}

export interface QualityBlock {
  startMs: number
  endMs: number
  level: MonitorLevel
  /** 成功样本的平均延迟（微秒），无成功样本时为 null */
  avgUs: number | null
  minUs: number | null
  maxUs: number | null
  /** 丢包率百分比 0–100 */
  lossPct: number
  total: number
  success: number
}

/** 把 now 对齐到块边界，让色条每 blockMs 才平移一次，避免实时刷新时抖动。 */
export function alignNow(nowMs: number, blockMs: number): number {
  return Math.floor(nowMs / blockMs) * blockMs + blockMs
}

/**
 * 按“探测次数”聚合（不是按时间加权）：逐块累加 total_count / success_count / res_sum。
 * 这样任意 interval 都正确；total === 0 的块是 empty（没探测过），绝不是丢包。
 */
export function buildQualityBlocks(
  records: RawMonitorStatsRecord[],
  spec: BlockSpec,
  nowMs: number,
  thresholds: MonitorThresholds
): QualityBlock[]

/** 块颜色判定，严格按此优先级（PRD §6.4.3） */
export function getBlockLevel(
  block: Pick<QualityBlock, "total" | "success" | "avgUs">,
  thresholds: MonitorThresholds
): MonitorLevel {
  if (block.total === 0) return "empty"            // ① 没探测过 → 灰
  if (block.success === 0) return "loss"           // ② 全丢 → 红
  const lossPct = ((block.total - block.success) / block.total) * 100
  if (lossPct >= 50) return "loss"                 // ③ 丢一半以上 → 红
  return getMonitorLevel(block.avgUs, thresholds)  // ④ 按成功样本平均分绿/黄/红
}

/** 0 < 丢包 < 50%：主色按延迟，另加底部红边 */
export function hasPartialLoss(block: Pick<QualityBlock, "total" | "success">): boolean {
  if (block.total === 0 || block.success === 0) return false
  const lossPct = ((block.total - block.success) / block.total) * 100
  return lossPct > 0 && lossPct < 50
}
```

**必须写的单测**（`monitor-blocks.test.ts`）：

1. 60s interval → `{blockMs:120000, type:"1m"}`
2. 300s interval → `{blockMs:600000, type:"10m"}`
3. 3 条记录落在同一块 → `total=3, success=2, sumUs` 相加，`avgUs = sum/2`
4. **一条记录都没有的块 → `level === "empty"`，不是 `"loss"`**（最关键）
5. 块内全丢（success=0）→ `level === "loss"`
6. 块内丢 1/3 且延迟 100ms → `level === "good"` + `hasPartialLoss === true`
7. 块内丢 3/4 → `level === "loss"` + `hasPartialLoss === false`
8. `alignNow` 幂等：连续调用两次（同一 blockMs）结果相同

#### P4.2 数据 Hook：`lib/use-monitor-cards.ts`（新建）

```ts
interface UseMonitorCardStatsProps {
  /** 当前可见（已筛选）的监控 */
  monitors: NetworkMonitorRecord[]
  thresholds: MonitorThresholds
}

/** 返回 monitorId -> 20 个色块。一次查询 + 一个订阅。 */
export function useMonitorCardStats(props: UseMonitorCardStatsProps): Map<string, QualityBlock[]>
```

实现要点：

- **按 `type` 分两组**（`1m` / `10m`），每组一次 `getFullList`。最多 2 次请求，**绝不能**每监控一次。
- filter 用 `monitorIdsFilter`（`use-network-monitors.ts` 里已有，需要 `export` 它，或复制到 `network-monitor-utils.ts`）。`monitorIds > 100` 时按当前页可见系统截断。
- `fields: "monitor,res_min,res_max,total_count,success_count,res_sum,created,type"`。
- `from` 计算：`alignNow(now, spec.blockMs) - spec.blockMs * 20`。
- **一个** realtime 订阅（`network_monitor_stats` 的 `create`，**不带 `type` 过滤**），回调按 `event.record.type` 分流到两个 buffer，200ms 批量 flush（复制 `useNetworkMonitorStats` 的 `pendingRaw` 模式）。
- 每 60s 用一个 `setInterval` 重算 `nowMs`（让色条随时间右移），`visibilitychange` 隐藏时暂停。
- 返回 `Map` 用 `useMemo`，且只有某监控最后一条 `created` 变化时才重建该条，避免整墙重渲染。

> ⚠️ **不要**在卡片里循环调用 `useNetworkMonitorStats`（N 个监控 = N 个订阅 + N 次请求）。

#### P4.3 色块组件：`components/network-monitors-table/quality-blocks.tsx`（新建）

```tsx
const levelBg: Record<MonitorLevel, string> = {
  good: "bg-emerald-500",
  warn: "bg-yellow-500",
  crit: "bg-red-500",
  loss: "bg-red-600",
  empty: "bg-muted",
}

export const QualityBlocks = memo(function QualityBlocks({
  blocks,
}: { blocks: QualityBlock[] }) {
  return (
    <div className="flex h-3 items-stretch gap-[2px]" role="img" aria-label={...}>
      {blocks.map((b, i) => (
        <div
          key={i}
          title={formatBlockTooltip(b)}   // 12:34–12:36 · 平均 168ms · 丢包 1/3
          className={cn(
            "flex-1 rounded-[1px]",
            levelBg[b.level],
            hasPartialLoss(b) && "border-b-2 border-red-700"
          )}
        />
      ))}
    </div>
  )
})
```

要点：

- **纯 `div`，不用 SVG、不用 Recharts**。20 个 div 的成本可以忽略。
- `blocks` 数组**从左到右 = 从旧到新**（渲染前把 `idx` 反序，因为聚合时 `idx=0` 是最新块）。
- RTL：`container` 上加 `rtl:flex-row-reverse`，保证"右边是最新"。
- 无障碍：整个色条 `role="img"` + `aria-label`（"最近 40 分钟延迟质量，绿色 12 块，黄色 5 块，红色 3 块"）。
- 空块（`empty`）也要渲染（灰色占位），保持 20 格视觉整齐。

#### P4.4 监控块与卡片墙：`components/network-monitors-table/monitor-card-grid.tsx`（新建）

**单监控块**（对齐 Komari 截图）：

```tsx
<button onClick={() => onOpenSheet(monitor)} className="text-start rounded-lg border p-2.5 hover:bg-accent/50">
  {/* 第 1 行：名称 + 协议 */}
  <div className="flex items-center justify-between gap-2">
    <span className="truncate text-xs font-medium">{getMonitorDisplayName(monitor)}</span>
    <span className="shrink-0 rounded bg-muted px-1 py-px text-[10px] uppercase text-muted-foreground">
      {getMonitorProtocolLabel(monitor)}
    </span>
  </div>
  {/* 第 2 行：延迟 */}
  <div className="mt-1.5 flex items-baseline justify-between gap-2">
    <span className="text-[11px] text-muted-foreground"><Trans>Latency</Trans></span>
    <span className={cn("text-sm font-semibold tabular-nums", levelText[avgLevel])}>
      {latencyText}
    </span>
  </div>
  {/* 第 3 行：丢包率 */}
  <div className="flex items-baseline justify-between gap-2">
    <span className="text-[11px] text-muted-foreground"><Trans>Loss</Trans></span>
    <span className={cn("text-sm font-semibold tabular-nums", monitor.loss1h > 0 ? "text-red-600 dark:text-red-400" : "text-muted-foreground")}>
      {monitor.loss1h.toFixed(1)}%
    </span>
  </div>
  {/* 第 4 行：色块 */}
  <QualityBlocks blocks={blocks} />
</button>
```

**数字口径**（照 PRD §6.4.4）：

```ts
const isTotalLoss = monitor.resAvg1h === 0 && monitor.loss1h >= 100
const hasNoData = monitor.resAvg1h === 0 && monitor.loss1h === 0
const latencyText = hasNoData ? "—" : isTotalLoss ? t`Loss` : formatMicroseconds(monitor.resAvg1h)
// 注意：用 resAvg1h，不是 res
```

**卡片**：

```tsx
<div className="grid gap-4 grid-cols-1 sm:grid-cols-2 lg:grid-cols-3">
  {systems.map((s) => {
    const list = monitorsOf(s).sort((a, b) => b.loss1h - a.loss1h)   // 最差的排前面
    const shown = list.slice(0, 3)
    return (
      <Card key={s.id}>
        <CardHeader className="flex-row items-center justify-between pb-2">
          <CardTitle className="flex items-center gap-2 text-base">
            <span className={cn("size-2 rounded-full", statusDot)} />
            {s.name}
          </CardTitle>
          <MonitorCardMenu system={s} monitors={list} />
        </CardHeader>
        <CardContent className="@container grid gap-2" style={{ gridTemplateColumns: undefined }}>
          <div className={cn("grid gap-2", getMonitorGridClass(shown.length))}>
            {shown.map((m) => <MonitorBlock key={m.id} monitor={m} blocks={blocksByMonitor.get(m.id)} ... />)}
          </div>
          {list.length > 3 && (
            <button onClick={() => showInTable(s.id)} className="text-start text-xs text-muted-foreground hover:text-foreground">
              +{list.length - 3} <Trans>more monitors</Trans>
            </button>
          )}
        </CardContent>
      </Card>
    )
  })}
</div>
```

**网格类函数（必须照抄，不要自行改断点）**：

```ts
function getMonitorGridClass(cols: number) {
  if (cols <= 1) return "grid-cols-1"
  if (cols === 2) return "grid-cols-1 @[340px]:grid-cols-2"
  return "grid-cols-1 @[340px]:grid-cols-2 @[560px]:grid-cols-3"
}
```

> 窄容器降级为 1 列是**有意为之**：系统卡片内容宽 350–420px 时三等分只有 110–140px，放不下「延迟 / 丢包率」两行 + 20 块色条。不要为了"一定横排 3 个"而压缩信息。

**卡片右上角菜单**：`AddMonitorDialog`（带 systemId）、全部启用/全部暂停、跳转系统详情（`getPagePath($router,"system",{id})`）。

#### P4.5 Sheet 抽取 + 视图切换接线

1. **先把 `NetworkMonitorSheet` 从 `network-monitors-table.tsx` 抽到独立文件** `components/network-monitors-table/network-monitor-sheet.tsx` 并 `export`（表格和卡片都要用它）。这一步单独提交，纯搬迁不改逻辑。
2. `types.d.ts::UserSettings` 加 `monitorViewMode?: "table" | "grid"`。
3. `network-monitors-table.tsx`（`NetworkMonitorsTableNew`）：
   - 顶部工具栏加 `LayoutList / LayoutGrid` 的 `DropdownMenuRadioGroup`（照抄 `systems-table.tsx:274`），`handleViewModeChange` 走三级回退：`$userSettings` → `localStorage["besz-monitor-view"]` → 默认 `table`。
   - `viewMode==="grid"` 时渲染 `MonitorCardGrid`，**保留**全局筛选输入（`globalFilter` 先过滤 `monitors` 数组，卡片视图也享受筛选）。
   - 卡片视图**隐藏**列设置/排序分组（只对表格有意义）；保留 `DataTablePagination`（用同一 table 实例的 `rows` 分页）。
4. `routes/monitors.tsx`、`routes/system.tsx` 无需改动（都渲染同一个 `NetworkMonitorsTableNew`）。

### 测试

1. `monitor-blocks.test.ts`：P4.1 列的 8 个用例（**第 4 条 empty≠loss 必须过**）。
2. `monitor-thresholds.test.ts`：`getMonitorLevel` 边界（150_000 / 400_000 / 900_000 微秒）。
3. 手动矩阵：
   - 0 系统 / 1 系统 1 监控 / 1 系统 3 监控 / 1 系统 5 监控（看 `+2 more`）/ 8 系统
   - 监控 interval = 60s、120s、300s（验证色条是否都饱满）
   - 刚添加、无数据的监控（20 块全灰 + `—`）
   - 把一个目标打到 100% 丢包（色条全红）
   - 窄屏（<640px）/ 中屏 / 宽屏；深色 / 浅色 / RTL
4. 性能：脚本插 200 个监控 + 40 分钟数据，滚动卡片墙观察帧率与网络面板（应只有 2 个 stats 请求）。

### 验收

PRD §11「卡片视图」全部 10 条。

### 风险

| 风险 | 应对 |
|---|---|
| 把 `empty` 当 `loss` | 5 分钟探测的监控会显示大面积"丢包"。单测用例 4 专门守这个 |
| 每监控一个请求/订阅 | 只允许 2 次 `getFullList` + 1 个订阅；code review 时数一下 |
| 色条每 2 分钟整体跳变 | 用 `alignNow` 对齐块边界，不是 `now` 直接算 |
| 卡片内横排 3 个太挤 | 用 `getMonitorGridClass` 的容器查询降级到 1 列，不要压缩字号到 9px |
| Sheet 复用 | 必须先抽成独立文件，否则卡片和表格会各写一份 |
| 200 监控卡顿 | 优先 `memo` + 减少 `Map` 重建；不要上虚拟列表 |

### 提交

```
stage-4a: extract network monitor sheet into its own module
stage-4b: add quality block aggregation helpers and tests
stage-4c: add monitor card stats hook
stage-4d: add quality blocks and monitor card grid
stage-4e: add table/grid view switch to network monitors
```

---

## 阶段 P5：计费数据模型 + 后端（无 UI）

### 目标

建 `system_billing` / `system_traffic` 两个集合，配好权限，提供后端读取模型。**本阶段不做任何前端，也不做统计逻辑**，只让数据“能存、能读、能改”。

### 依赖

无（可与 P1–P4 并行）

### 改动清单

#### 1. 迁移 `internal/migrations/<ts>_system_billing.go`（新建集合）

集合 JSON 参考 `0_collections_snapshot_0_20_0.go` 的写法，字段见 PRD §4.2 / §5.2.1。

关键字段定义（写成 Go struct 形式供参考，实际用集合 JSON）：

| Name | Type | 参数 |
|---|---|---|
| `system` | relation | `CollectionId: <systems 集合 id>`, `MaxSelect:1`, `CascadeDelete:true` |
| `billingType` | select | values `free`,`paid`；MaxSelect 1 |
| `price` | number | `OnlyInt:false` |
| `currency` | select | `CNY`,`USD`,`EUR`,`GBP` |
| `cycle` | select | `monthly`,`semiannual`,`annual`,`biennial`,`triennial`,`permanent` |
| `paidAt` | date | — |
| `expiresAt` | date | — |
| `notifyDaysBefore` | number | `OnlyInt:true`, `Min:0`, `Max:60` |
| `remindLevel` | number | `OnlyInt:true`（7/3/1/0/-1） |
| `remindAt` | date | — |
| `trafficLimit` | number | `OnlyInt:true`, `Min:0` |
| `trafficMode` | select | `sum`,`upload` |
| `resetDay` | number | `OnlyInt:true`, `Min:1`, `Max:31` |
| `trafficAlerts` | bool | — |
| `trafficRemindLevel` | number | `OnlyInt:true` |
| `manualResetAt` | date | — |

索引：

```sql
CREATE UNIQUE INDEX `idx_billing_system` ON `system_billing` (`system`)
```

> `systems` 集合的 id 是 `2hz5ncl8tizk5nx`（见 snapshot）。新迁移里**不要硬编码**，用 `app.FindCollectionByNameOrId("systems")` 拿 id。

#### 2. 迁移 `internal/migrations/<ts>_system_traffic.go`（新建集合）

| Name | Type | 参数 |
|---|---|---|
| `system` | relation | cascade delete, MaxSelect 1 |
| `periodStart` | date | — |
| `periodEnd` | date | — |
| `usedUpload` | number | `OnlyInt:true` |
| `usedDownload` | number | `OnlyInt:true` |
| `lastCounters` | json | `MaxSize: 200000` |
| `lastSampleAt` | date | — |
| `lastResetAt` | date | — |
| `resetReason` | select | `auto`,`manual`,`init` |

索引：`CREATE UNIQUE INDEX idx_traffic_system ON system_traffic (system)`

#### 3. 权限 `internal/hub/collections.go`

在 `setCollectionAuthSettings` 里，紧挨 `network_monitors` 那段之后加：

```go
if err := applyCollectionRules(app, []string{"system_billing"}, collectionRules{
	list:   &systemScopedReadRule,
	view:   &systemScopedReadRule,
	create: &systemScopedWriteRule,
	update: &systemScopedWriteRule,
	delete: &systemScopedWriteRule,
}); err != nil {
	return err
}

// system_traffic 只由 Hub 写入（服务端 superuser 上下文），API 侧只读。
if err := applyCollectionRules(app, []string{"system_traffic"}, collectionRules{
	list: &systemScopedReadRule,
	view: &systemScopedReadRule,
}); err != nil {
	return err
}
```

> `applyCollectionRules` 的零值字段就是 `nil`，所以不传 create/update/delete 即为禁止。

#### 4. 后端 Go 模型 `internal/hub/billing/billing.go`（新建包）

```go
package billing

type Currency string // CNY USD EUR GBP
type Cycle string    // monthly semiannual annual biennial triennial permanent
type TrafficMode string // sum upload

// Config 是 system_billing 的类型化视图。
type Config struct {
	SystemId         string      `db:"system"`
	BillingType      string      `db:"billingType"`
	Price            float64     `db:"price"`
	Currency         Currency    `db:"currency"`
	Cycle            Cycle       `db:"cycle"`
	PaidAt           int64       `db:"paidAt"`
	ExpiresAt        int64       `db:"expiresAt"`
	NotifyDaysBefore int         `db:"notifyDaysBefore"`
	RemindLevel      int         `db:"remindLevel"`
	RemindAt         int64       `db:"remindAt"`
	TrafficLimit     int64       `db:"trafficLimit"`
	TrafficMode      TrafficMode `db:"trafficMode"`
	ResetDay         int         `db:"resetDay"`
	TrafficAlerts    bool        `db:"trafficAlerts"`
	TrafficRemindLevel int       `db:"trafficRemindLevel"`
	ManualResetAt    int64       `db:"manualResetAt"`
}
```

同时提供纯函数（**必须可单测，不依赖 DB**）：

```go
// DaysUntilExpiry 返回整天数（负数=已过期）。
func DaysUntilExpiry(expiresAt int64, now time.Time) int
// NextPeriod 计算 [periodStart, periodEnd)：按 resetDay 与 now 所在月。
func Period(now time.Time, resetDay int, loc *time.Location) (start, end time.Time)
// ReminderLevel 根据剩余天数返回档位：-1/0/1/3/notifyDaysBefore。
func ReminderLevel(daysLeft, notifyDaysBefore int) int
// CycleDuration 仅用于 UI 的"按周期推算到期日"。
func AddCycle(t time.Time, c Cycle) time.Time
```

**必须写进注释的边界**：
- `resetDay=31` 在 2 月取 28/29，在 4/6/9/11 月取 30。
- `AddCycle` 的 `permanent` 返回零值（无到期）。
- `DaysUntilExpiry` 用“当地 00:00 对齐”算整天，避免 23:59 与 00:01 差一天。

#### 5. 记录 Hook `internal/hub/billing/billing_hooks.go`（新建）

```go
// 当 expiresAt 变化时重置提醒档位，让续费后能重新提醒。
func BindBillingHooks(app core.App) {
	app.OnRecordUpdateRequest("system_billing").BindFunc(func(e *core.RecordRequestEvent) error {
		if e.Record.GetDateTime("expiresAt") != e.Record.Original().GetDateTime("expiresAt") {
			e.Record.Set("remindLevel", 0)
		}
		// price/cycle 校验：paid 且非 permanent 时 expiresAt 必填
		...
		return e.Next()
	})
}
```

在 `internal/hub/hub.go::StartHub` 里调用（与 `bindNetworkMonitorsEvents` 并列）。

### 测试

1. `billing_test.go`：
   - `Period(2026-03-05, 14)` → `2026-02-14 ~ 2026-03-14`
   - `Period(2026-03-14, 14)` → `2026-03-14 ~ 2026-04-14`
   - `Period(2026-02-10, 31)` → `2026-01-31 ~ 2026-02-28`
   - `ReminderLevel(7,7)=7`, `(6,7)=7`, `(3,7)=3`, `(2,7)=1`? → 注意：档位是“命中最近的一档”，用 `<=` 判断，`(2,7)` 命中 3 → 若已提醒 7 档则不再提醒（见 P7）
   - `DaysUntilExpiry` 边界（当天/次日/已过期）
2. 迁移测试：起 Hub 后 `sqlite3` 查 `system_billing` 表结构；写一条记录验证唯一索引生效。

### 验收

- 两个集合存在，权限符合 PRD §8（用 readonly 用户验证写被拒）。
- `go test ./internal/hub/billing/...` 全绿。
- 重启 Hub 迁移不报错（幂等）。

### 风险

- **迁移 id 冲突**：集合 JSON 里的 `id` 必须唯一（15 位）。可以省略让它自动生成，但要在同一迁移里能引用（用 `FindCollectionByNameOrId` 拿）。
- **时间字段**：PocketBase `date` 存的是字符串 `"2026-03-14 00:00:00.000Z"`，Go 侧用 `types.DateTime` 或 `GetDateTime`，不要直接 `GetString` 解析。

### 提交

```
stage-5: add system_billing and system_traffic collections
```

---

## 阶段 P6：计费前端（表单 + 卡片展示）

### 目标

系统卡片横点菜单出现「计费与流量」，可配置并展示；卡片显示价格/周期/剩余天数。

### 依赖

P3（卡片渲染已稳定）、P5（集合存在）

### 改动清单

#### 1. 数据层 `lib/billing.ts`（新建）

```ts
export interface BillingRecord { id: string; system: string; billingType: ...; ... }
export interface TrafficRecord { id: string; system: string; periodStart: string; ... }

/** 全局 atom：systemId -> BillingRecord */
export const $billingBySystemId = atom<Record<string, BillingRecord>>({})
/** 全局 atom：systemId -> TrafficRecord */
export const $trafficBySystemId = atom<Record<string, TrafficRecord>>({})

/** 在 systems-table 挂载时调用一次：拉全量 + 订阅。 */
export function useBillingSync(enabled = true)
```

实现要点：
- `pb.collection("system_billing").getFullList()` + `subscribe("*")`（与 `useNetworkMonitors` 同模式，50ms 批量）。
- `system_traffic` 同理，但**注意频率**：每系统每分钟 1 次更新 → 订阅回调要批处理 + 用 `requestAnimationFrame` 或 300ms 节流，避免频繁重渲染整个卡片墙。
- 只保留当前可见系统的记录（避免 500 系统时全量常驻内存 —— 个人使用可先不做，但要留 TODO）。

#### 2. 纯函数 `lib/billing-utils.ts`（新建，必须可单测）

```ts
export const CURRENCY_SYMBOL: Record<Currency, string> = { CNY: "¥", USD: "$", EUR: "€", GBP: "£" }
export const CYCLE_LABEL: Record<Cycle, string>  // 走 lingui，注意：这些要放在组件层用 t`` 包裹
export function formatPrice(price: number, currency: Currency): string   // 25 → "¥25"，25.5 → "¥25.50"
export function formatCycleShort(cycle: Cycle): string                   // monthly → "/mo"
export function getExpiryState(expiresAt: number, now = Date.now()):
  { kind: "none" | "free" | "permanent" | "active" | "expired"; daysLeft?: number }
export function getExpirySeverity(daysLeft: number): "ok" | "warn" | "crit"  // >=30 / 7..29 / <7
export function formatBytesIEC(bytes: number): string                    // 812 GB / 1.02 TB
export function getTrafficState(limit: number, used: number, periodEnd: number, now = Date.now()):
  { pct: number; remaining: number; daysLeft: number; exceeded: boolean }
```

> `formatBytesIEC` 若项目已有类似工具（`lib/utils.ts` 里搜 `formatBytes`）优先复用。

#### 3. 对话框 `components/systems-table/billing-dialog.tsx`（新建）

```tsx
export function BillingDialog({ system, open, onOpenChange }: {...})
```

- Radix `Tabs`（`@radix-ui/react-tabs` 已在依赖里）→ 两个 Tab：`费用` / `流量`。
- 费用 Tab 字段见 PRD §4.3；「按周期推算」按钮：
  ```ts
  const next = addCycle(paidAt, cycle)  // 前端也要有一份同样的日历逻辑
  ```
  > 前端实现放 `billing-utils.ts`，**必须与 Go 的 `AddCycle` 行为一致**（月加、年加、月末裁剪）。写单测覆盖 1/31 + 1 月。
- 保存：`pb.collection("system_billing").update(id, payload)` 或 `create`（无记录时）。
- readonly：`isReadOnlyUser()` → 全部 `disabled` + 隐藏保存按钮。
- 保存后 toast + atom 立即更新（乐观更新，避免等订阅）。

#### 4. 入口 `systems-table-columns.tsx::ActionsButton`

菜单加一项（在「编辑」之后）：

```tsx
<DropdownMenuItem onSelect={() => setBillingOpen(true)}>
  <WalletIcon className="me-2 size-4" />
  <Trans>Billing & Traffic</Trans>
</DropdownMenuItem>
```

并渲染 `<BillingDialog system={system} open={billingOpen} onOpenChange={setBillingOpen} />`。
注意 `ActionsButton` 整体在 `useMemo` 里，`billingOpen` 要加进依赖数组。

#### 5. 卡片展示 `systems-table-columns.tsx`

新增一个非排序的“计费”渲染块。两种视图都要有：

- **grid**：在 `SystemCard` 的现有行下方追加一个 `BillingSummary` 组件（含价格行 + 流量进度条）。
- **table**：新增列 `billing`（`id: "billing"`，默认 `hidden`），显示紧凑文案。

`BillingSummary` 结构：

```tsx
<div className="flex flex-col gap-1.5 border-t pt-2 text-xs">
  <div className="flex items-center gap-1.5">
    <WalletIcon className="size-3.5 text-muted-foreground" />
    <span className={severityClass}>{priceText}</span>
    {expiryText && <span className="text-muted-foreground">·</span>}
    <span className={severityClass}>{expiryText}</span>
  </div>
  {traffic && <TrafficBar ... />}
</div>
```

无 `system_billing` 记录时**整块不渲染**（不占位）。

#### 6. 加载顺序问题

`systems-table.tsx` 里调用 `useBillingSync()`。卡片渲染时 `$billingBySystemId.get()[system.id]` 可能还没到 → 首帧不显示，随后出现。**不要**用骨架闪烁（数据量小，直接不渲染）。

### 测试

1. `billing-utils.test.ts`：`formatPrice`、`getExpiryState`、`getTrafficState`、`addCycle`（含月末）。
2. 手动：4 种货币 × 6 种周期 × 3 种到期状态（正常/临期/过期）全组合看一眼。
3. readonly 用户：菜单可见、对话框只读。

### 验收

PRD §11「成本」前 4 条。

### 提交

```
stage-6a: add billing data layer and utils
stage-6b: add billing dialog and card summary
```

---

## 阶段 P7：到期提醒 Cron

### 目标

每天一次扫描，命中档位发通知，带去重。**通知的开关复用现有「警报」系统**：在系统卡片的「警报」面板新增一个 `BillingExpiry` 开关（每个用户可按系统开启），Cron 只给打开了该开关的用户发送——与「证书到期」完全同一套机制，不再"直接给系统全部用户群发"。

### 依赖

P6（能配置才有意义；严格说 P5 就够，但 P6 之后才好手测）

### 改动清单

#### 0. 新增告警类型 `BillingExpiry`（接入现有「警报」面板）

这是本阶段的核心设计变更：**不再新增 `system_billing.expiryAlerts` 布尔字段，也不群发**，而是复用站点已有的按用户/按系统的告警开关。

- **迁移**：照抄 `internal/migrations/1791926360_network_monitor_cert_alert.go`，把 `BillingExpiry` 追加到 `alerts.name` 的 `SelectField.Values`（下迁移里删除）。无需改 `system_billing`。
- **前端** `internal/site/src/lib/alerts.ts` 的 `alertInfo` 增加一项（纯开关，无阈值滑块、无持续时长，和 `Status`/`SystemdFailed` 一样）：
  ```ts
  BillingExpiry: {
    name: () => t`Billing Expiration`,
    unit: "",
    icon: WalletIcon,
    desc: () => t`Triggers when a paid system nears its expiry date`,
    // 纯开关：阈值在计费对话框的「提前 N 天」里，面板里只负责"要不要收"
    noDuration: true,
    noThreshold: true,
  },
  ```
  `noThreshold && noDuration` ⇒ `hasControls=false`，渲染成只有一个 Switch（与截图里的「证书到期」同一行样式）。
- **阈值来源**：沿用 `system_billing.notifyDaysBefore`（P6 表单里的「提前 N 天」），不在警报面板重复设置。
  > 备选方案：像 `NetworkMonitorCert` 一样把天数塞进 `alerts.value`。但那会和 P6 对话框的 `notifyDaysBefore` 形成两个真相，先不采用；若以后想去掉 `notifyDaysBefore` 再切。

#### 1. `internal/hub/billing/reminders.go`（新建，实际文件名）

```go
// CheckExpiryReminders 每日到期提醒任务（实际签名返回 error，供 Cron 记日志）。
func (bm *Manager) CheckExpiryReminders() error {
	billingRecords, err := bm.app.FindAllRecords("system_billing",
		dbx.NewExp("billingType = 'paid' AND cycle != 'permanent' AND expiresAt != ''"))
	...
	now := time.Now()
	for _, rec := range billingRecords {
		cfg := configFromRecord(rec)
		daysLeft := DaysUntilExpiry(cfg.ExpiresAt, now)
		level := ReminderLevel(daysLeft, cfg.NotifyDaysBefore)
		// remindLevel 的 0 同时是"未提醒过"的哨兵，不能直接比大小
		if level == LevelNone || (cfg.RemindLevel != LevelNone && level >= cfg.RemindLevel) {
			continue
		}
		system, err := bm.app.FindRecordById("systems", cfg.SystemId)
		...
		title, message := buildExpiryMessage(system.GetString("name"), cfg, daysLeft)
		// 只发给打开「BillingExpiration」告警的用户（每个 user+system 一条 alerts 记录）
		subs, err := bm.app.FindAllRecords("alerts", dbx.HashExp{"system": cfg.SystemId, "name": "BillingExpiry"})
		if err != nil {
			continue
		}
		for _, a := range subs {
			_ = bm.alerts.SendAlert(alerts.AlertMessageData{
				UserID: a.GetString("user"), SystemID: cfg.SystemId,
				Title: title, Message: message,
				Link: bm.hubUrl() + "/system/" + cfg.SystemId, LinkText: "View system",
			})
		}
		rec.Set("remindLevel", level)
		rec.Set("remindAt", now)
		_ = bm.app.SaveNoValidate(rec)   // 注意：不要用 Save（会触发用户侧校验）
	}
}
```

要点：
- **先发通知再更新 `remindLevel`**，还是反过来？→ **先更新再发**更安全（避免发送成功但保存失败导致重复轰炸）；但会导致“发失败也标记已发”。个人使用选**先更新**，并在日志里记录失败。
- 一天只跑一次，且 `level != 0 && level < remindLevel` 才发 → 天然幂等（同一天重复执行不会重发，因为 `remindLevel` 已推进）。
- ⚠️ **`remindLevel` 的初值就是 `0`（`LevelNone`）**，所以判据必须排除 `remindLevel == 0` 的情况：单纯写 `level >= remindLevel` 会让首次运行（`7 >= 0`）直接跳过，一条都发不出去。实际实现见 `reminders.go`。
- **没有订阅者时不推进 `remindLevel`**：否则用户后来打开开关会永远错过当前档位。
- **不要**在循环里开事务（一次 Save 足够，个人规模）。
- **按用户开关、按记录去重**：开关是每个 `user+system` 的 `alerts` 记录；去重状态 `remindLevel` 仍在 `system_billing`（全局）。单用户系统完全正确；多用户时"某用户后开启 BillingExpiry"可能漏掉已推进的档位 —— 个人部署可接受，记为已知限制。若要严格按用户去重，需给 `alerts` 的 `state` JSON 记录每个用户已提醒到的档位（参考 `NetworkMonitorCert` 的 `state` 用法）。

#### 2. 注册 Cron `internal/hub/hub.go::registerCronJobs`

```go
h.Cron().MustAdd("billing expiry reminders", "30 0 * * *", h.bm.CheckExpiryReminders)
```

> 时间选择：UTC 00:30。README 里注明“请设置 `TZ` 环境变量”。

#### 3. 消息文案

```
System expires in 7 days: web-01
web-01 (¥25/mo) expires on 2026-03-14 (7 days left). Update the expiry date after renewing.
link: http://hub:8090/system/xxxx
```

到期当天与已过期时：

```
System expires today: web-01
web-01 (¥25/mo) expires today (2026-03-14). Update the expiry date after renewing.

System expired: web-01
web-01 (¥25/mo) expired on 2026-03-14 (3 days ago). Update the expiry date after renewing.
```

**为什么是英文，以及为什么不该去 Go 里做 i18n**：Beszel 的通知正文全部在 Go 侧、投递前拼成字符串（`alerts_system.go`、`alerts_container.go`、`alerts_network_monitors.go`、`alerts_zfs.go`、`alerts_smart.go`、`alerts_systemd.go` 无一例外都是 `fmt.Sprintf` 英文模板），再经 `AlertManager.SendAlert` 交给 shoutrrr / SMTP。触发方是 Cron 或心跳入库，**整条链路里没有浏览器**，所以前端 Lingui 目录碰不到这些文案。前端负责的是展示层（`lib/alerts.ts::alertInfo` 的 name/unit/desc、告警历史表），那部分本来就走 Lingui。`reminders.go::expiryMessage` 与上游保持同一模式。

真要做多语言邮件，属于**平台级改造**而不是 P7 的补丁：Go 侧加按语言分发的消息表 + 语言来源（`user_settings.settings.lang`，注意目前全仓没有代码写这个字段，`api.ts:94` 只读不写）+ 逐个迁移 6 个告警类型。已记录在 `docs/FOLLOWUPS.md`。

### 测试

1. `billing_reminders_test.go`（用 `beszelTests` 建测试数据，参考 `alerts_status_test.go`）：
   - `daysLeft=7`, `remindLevel=0` → 发送一次，`remindLevel=7`
   - 再跑一次 → 不发送
   - `daysLeft=3` → 发送一次，`remindLevel=3`
   - `daysLeft=1` → 发送，`remindLevel=1`
   - `daysLeft=0` → 发送，`remindLevel=-1`
   - 改 `expiresAt` 到 10 天后 → hook 把 `remindLevel` 归零 → 再跑不发送（因为 `level=0`）
   - `billingType=free` → 不发
   - 多用户系统 → 每个用户各发一次
2. 手动：把某系统 `expiresAt` 改成明天，把 Cron 临时改成 `* * * * *` 观察，改回来。

### 验收

PRD §11「成本」后 3 条。

### 风险

- `system.GetStringSlice("users")` 对 relation 字段返回的是 id 列表（PocketBase 行为），确认一下再写。
- `SaveNoValidate` 与 `Save` 的区别：`Save` 会跑 collection 校验规则（`notifyDaysBefore` 范围等），改内部字段用 `SaveNoValidate` 更稳。

### 提交

```
stage-7: add daily billing expiry reminders
```

---

## 阶段 P8：流量统计后端

### 目标

Hub 每采样累计流量、按周期滚动、支持手动重置。

### 依赖

P5（集合）、P7（Manager 已存在）

### 改动清单

#### 1. `internal/hub/billing/traffic.go`（新建）

```go
// AccumulateTraffic 在 system_stats 落库的同一事务内调用。
// stats 是本次采样的网卡累计计数器（system.Stats.NetworkInterfaces）。
func (bm *Manager) AccumulateTraffic(txApp core.App, systemId string, stats *system.Stats, now time.Time) error
```

实现严格按 PRD §5.3 伪代码：

```go
cfg, traffic, err := loadConfigAndTraffic(txApp, systemId)
if cfg == nil || cfg.TrafficLimit == 0 { return nil }

// 1) 周期滚动 / 手动重置
needReset := traffic == nil ||
    traffic.PeriodEnd == 0 ||
    !now.Before(traffic.PeriodEnd) ||
    (cfg.ManualResetAt > 0 && cfg.ManualResetAt > traffic.LastResetAt)

if needReset {
    start, end := Period(now, cfg.ResetDay, time.Local)
    // 手动重置不改变周期边界，只清量；自动滚动才顺延
    reason := "auto"
    if traffic != nil && cfg.ManualResetAt > traffic.LastResetAt { reason = "manual"; }
    if traffic == nil { reason = "init" }
    if reason == "manual" && traffic != nil {
        start, end = traffic.PeriodStart, traffic.PeriodEnd
    }
    traffic.UsedUpload, traffic.UsedDownload = 0, 0
    traffic.PeriodStart, traffic.PeriodEnd = start, end
    traffic.LastResetAt = now
    traffic.ResetReason = reason
    traffic.LastCounters = map[string][2]uint64{}   // 清基线，下面重建
}

// 2) 逐网卡差分
counters := map[string][2]uint64{}
var upDelta, downDelta uint64
for name, v := range stats.NetworkInterfaces {
    cur := [2]uint64{v[2], v[3]}
    if prev, ok := traffic.LastCounters[name]; ok {
        upDelta += safeDelta(prev[0], cur[0])
        downDelta += safeDelta(prev[1], cur[1])
    }
    counters[name] = cur
}
traffic.LastCounters = counters
traffic.UsedUpload += int64(upDelta)
traffic.UsedDownload += int64(downDelta)
traffic.LastSampleAt = now

return saveTraffic(txApp, traffic)
```

`safeDelta`：

```go
const maxTrafficDeltaBytes uint64 = 10 << 40 // 10 TiB

// safeDelta 返回计数器增量；cur < prev 视为计数器归零（机器重启），返回 cur。
func safeDelta(prev, cur uint64) uint64 {
	if cur >= prev {
		d := cur - prev
		if d > maxTrafficDeltaBytes {
			return 0 // 异常保护（驱动 bug / 网卡热插拔）
		}
		return d
	}
	return cur
}
```

⚠️ **注意 `lastCounters` 的 JSON 形态**：`map[string][2]uint64` 序列化成 `{"eth0":[123,456]}`。PocketBase json 字段本身是 `any`，用 `rec.UnmarshalJSONField("lastCounters", &m)` / `rec.Set("lastCounters", m)`。

#### 2. 接入 `internal/hub/systems/system.go::createRecords`

在 `systemStatsRecord` 保存之后、`system_details` 之前（**同一事务内**）：

```go
if err := sys.manager.billing.AccumulateTraffic(txApp, sys.Id, &data.Stats, time.Now()); err != nil {
	return err   // 是否要因为流量统计失败而丢弃整条 system_stats？
}
```

> **设计决定**：流量统计失败**不应该**导致 `system_stats` 丢失（那是核心监控数据）。
> 因此这里**不要 return err**，改为 `sys.manager.hub.Logger().Error("accumulate traffic", ...)` 并继续。事务里只做本系统内的写入，失败风险主要是 json 反序列化，概率极低。

#### 3. `SystemManager` 持有 `billing *billing.Manager`

在 `internal/hub/systems/system_manager.go` 的 `SystemManager` 结构体加字段，并在 `NewSystemManager` 时注入（`hub.go` 里创建顺序注意：billing manager 要先于 systems manager，或晚注入）。

#### 4. 手动重置的可见性

`manualResetAt` 由前端写；Hub 在下一个采样点看到 `manualResetAt > lastResetAt` 就重置。**重置后要把 `manualResetAt` 清空**（或不动也行，因为 `lastResetAt` 会更新到 now，之后不会再触发）。建议**不清空**，保持"操作历史"，靠 `lastResetAt` 比较天然幂等。

### 测试

1. `traffic_test.go`（纯函数 + 用内存 DB）：
   - 首次采样（无 traffic 记录）→ `used=0`，`lastCounters` 建立，`resetReason=init`
   - 第二次采样 +1000 up / +500 down → `usedUpload=1000`, `usedDownload=500`
   - 计数器归零（模拟重启）→ 增量 = 当前值，`used` 不回退
   - 网卡消失 → 只算剩余网卡的增量，不下溢
   - 新增网卡 → 首次只建基线
   - 跨周期（把 `periodEnd` 改到过去）→ 归零 + 顺延
   - `manualResetAt > lastResetAt` → 归零，周期边界不变
   - 单次差值 > 10 TiB → 丢弃
2. 集成：起 Hub + Agent，`curl` 下载 500MB，看 `system_traffic.usedDownload` 增长。

### 验收

PRD §11「流量」中「造 500MB」「停 Hub 2 小时」「重启机器」「仅计上行」「跨重置日」「resetDay=31」「立即重置」几条。

### 风险

- **事务内做额外查询**：`loadConfigAndTraffic` 会在事务里多 2 次查询（每系统每分钟 2 次）。个人规模无感；若系统很多，可加内存缓存（**本阶段不做**，写进 FOLLOWUPS）。
- **时间字段比较**：`traffic.PeriodEnd` 从 DB 读出来是 `types.DateTime`，与 `time.Time` 比较要统一用 `Time()`。
- **`system.Stats` 结构变化**：`NetworkInterfaces` 可能为 nil（Agent 未上报）→ 要判空，直接 `return nil`（不建立基线）。

### 实际实现与本文的差异（2026-10-25）

- `AccumulateTraffic` 落在 `internal/hub/billing/traffic.go`，但**没有**把 `billing *billing.Manager` 塞进 `SystemManager`。改成 `systems.TrafficAccumulator` 接口 + `SetTrafficAccumulator`，`hub.NewHub` 注入 —— `systems` 包不 import `billing`，测试里不注入时整条路径自然跳过。
- **先修了 `ParseCounters` 才能跑通**：`record.Get("lastCounters")` 返回 `types.JSONRaw`（`[]byte`）而不是 `map[string]any`，P5 的实现会静默返回空 map，差分恒为 0。已在 `ParseCounters` 里加 JSONRaw / `[]byte` 分支（见 PROGRESS 偏差 31）。
- 手动重置**保留周期边界**（PRD §5.3 伪代码是重算），且自动滚动优先于手动重置（PROGRESS 偏差 34）。
- 写入用 `SaveNoValidate`；累计失败只 `Log.Error` 不 return（偏差 32、35）。
- 测试拆两份：`billing/traffic_test.go`（真实 DB 驱动采样器，7 个场景）与 `billing/traffic_pure_test.go`（`safeDelta` / `resetReason`），另有 `systems/system_traffic_test.go` 验证 `createRecords` 的接线。

### 提交

```
stage-8: accumulate per-period traffic on the hub
```

---

## 阶段 P9：流量前端

### 目标

流量 Tab 可配置、卡片显示进度条与倒计时、可手动重置。

### 依赖

P6（对话框框架）、P8（后端已产数据）

### 改动清单

1. `billing-dialog.tsx` 的 `流量` Tab：字段见 PRD §5.5。
   - 单位选择用 `Select`（MB/GB/TB/PB），内部换算成字节存 `trafficLimit`。
   - 「立即重置」按钮：`pb.collection("system_billing").update(id, { manualResetAt: new Date().toISOString() })`，二次确认 `AlertDialog`，提示「下一个采样周期（≤60 秒）内生效」。
2. `TrafficBar` 组件（放 `billing-dialog.tsx` 同级或 `lib` 旁）：
   - `role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}`
   - 颜色：<80 正常 / 80–99 黄 / ≥100 红
   - 文案：`812 GB / 1 TB · 18 天后重置`
   - 悬浮 tooltip：上下行拆分 + 周期区间 + 上次采样时间
3. 卡片接线：`BillingSummary` 里读 `$trafficBySystemId`，有记录才渲染 `TrafficBar`。
4. 暂停系统：`system.status === paused` → `TrafficBar` 加 `opacity-50` + tooltip「暂停期间不计流量」。

### 测试

手动矩阵：`trafficLimit=0` / 未用 / 50% / 85% / 100% / 超量 / 暂停系统 / 刚重置。

### 验收

PRD §11「流量」剩余条目。

### 提交

```
stage-9: add traffic config tab and card progress bar
```

---

## 阶段 P10（可选）：流量阈值提醒 / 流量历史 / 连接数

拆分清楚，每个都是**独立可跳过**的小任务。

> **v1.1 变更**：原 P10.3「per-monitor 阈值」**已取消** —— Q6 确认全局阈值，永不做每监控覆盖。

### P10.1 流量阈值提醒（**优先级最高，Q11 指定放最后一段**）

- 在 P7 的 Cron 里追加：读 `system_traffic` + `system_billing`，算 `pct`，`level = pct >= 100 ? -1 : pct >= 80 ? 80 : 0`，按 `trafficRemindLevel` 去重（规则同到期提醒：`level >= remindLevel` 则跳过）。
- 周期滚动时（P8 的重置逻辑）把 `trafficRemindLevel` 清 0。
- 消息：`web-01 has used 812 GB of 1 TB (81%) this period, resets in 18 days.`

### P10.2 流量历史曲线

- 新集合 `traffic_stats`（`system, type, up_bytes, down_bytes, created`），由 `internal/records/records.go` 的 10 分钟 Cron 落增量。
- 关键：**不能简单平均**，要存“相邻两次采样之间 `usedUpload/usedDownload` 的增量”。
- 前端在流量 Tab 里加一个小折线（复用 `LineChartDefault`）。

### P10.3（已取消）

~~per-monitor 阈值~~ —— Q6 已确认统一全局 150/400ms，不做每监控覆盖。

### P10.4 TCP / UDP 连接数（可选，仅 Linux）

**前提核查**：Agent 目前**完全没有**采集连接数（`system.Stats` 无相关字段，`agent/network.go` 只有 `psutilNet.IOCounters`）。这是**新增采集**，不是复用。

- 数据：`system.Stats` 加 `Connections [2]uint16`，`json:"conn,omitzero"` / `cbor:"43,keyasint,omitzero"`，语义 `[tcp, udp]`（cbor 42 已被 `SwapBytes` 占用）。
- 采集 `agent/network_conns_linux.go`（构建标签 `//go:build linux`）：

  ```go
  // countEstablishedTCP 统计 /proc/net/tcp{,6} 中 st == "01"（ESTABLISHED）的行数。
  // 只读文件、不反查进程，几千 socket 时 < 5ms。
  func countConnections() (tcp, udp uint16) {
      tcp = countNetFile("/proc/net/tcp", true) + countNetFile("/proc/net/tcp6", true)
      udp = countNetFile("/proc/net/udp", false) + countNetFile("/proc/net/udp6", false)
      return
  }
  ```

  `countNetFile(path string, establishedOnly bool)`：`bufio.Scanner` 逐行，跳过首行 header，按空白分割取第 4 列 `st`，`establishedOnly` 时只计 `01`。UDP 无连接状态，全部计入。
- 非 Linux：`agent/network_conns_unsupported.go` 返回 `0, 0`（`omitzero` 不上报，前端隐藏）。
- 展示：**系统卡片**（机器级指标），折叠为 `1.2k conns` 小字；或放系统详情页「网络」图表区顶部。
- **不要**用 `gopsutil net.Connections("tcp")`：它遍历 `/proc/*/fd` 反查 inode→进程，1000+ 连接时开销显著。

### 提交

```
stage-10a: add traffic usage threshold reminders
stage-10b: add traffic history records (optional)
stage-10c: collect tcp/udp connection counts on linux (optional)
```

---

## 附录 A：跨阶段文件影响矩阵

> 用途：判断“我要改这个文件，会不会踩到别的阶段”。

| 文件 | P1 | P2 | P3 | P4 | P5 | P6 | P7 | P8 | P9 |
|---|---|---|---|---|---|---|---|---|---|
| `internal/migrations/*` | ✅ | | | | ✅ | | | | |
| `internal/hub/collections.go` | | | | | ✅ | | | | |
| `internal/hub/hub.go` | | | | | ✅ | | ✅ | ✅ | |
| `internal/hub/network_monitors.go` | ✅ | | | | | | | | |
| `internal/hub/systems/system.go` | | | | | | | | ✅ | |
| `internal/hub/systems/system_manager.go` | | | | | | | | ✅ | |
| `internal/hub/billing/*` | | | | | ✅ | | ✅ | ✅ | ✅ |
| `agent/network_monitor_task.go` | | ✅ | | | | | | | |
| `agent/system.go` | | | ✅ | | | | | | |
| `internal/entities/system/system.go` | | | ✅ | | | | | | |
| `internal/alerts/alerts.go` | | | | | | | 只读 | | |
| `site/src/types.d.ts` | ✅ | | ✅ | ✅ | | ✅ | | | ✅ |
| `site/src/lib/use-network-monitors.ts` | ✅ | ✅ | | ✅ | | | | | |
| `site/src/lib/monitor-compare.ts` | ✅ | | | ✅ | | | | | |
| `site/src/lib/monitor-thresholds.ts` | | ✅ | | ✅ | | | | | |
| `site/src/lib/billing*.ts` | | | | | | ✅ | | | ✅ |
| `site/src/lib/monitor-blocks.ts` | | | | ✅ | | | | | |
| `site/src/lib/use-monitor-cards.ts` | | | | ✅ | | | | | |
| `site/src/components/network-monitors-table/*` | ✅ | ✅ | | ✅ | | | | | |
| `site/src/components/network-monitors-table/quality-blocks.tsx` | | | | ✅ | | | | | |
| `site/src/components/systems-table/systems-table-columns.tsx` | | | ✅ | | | ✅ | | | ✅ |
| `site/src/components/systems-table/systems-table.tsx` | | | | | | ✅ | | | |
| `site/src/components/routes/system/charts/monitors-charts.tsx` | ✅ | | | ✅ | | | | | |
| `agent/network_conns_linux.go` | | | | | | | | | P10.4 |

**冲突热点**：`systems-table-columns.tsx`（P3/P6/P9 都动）、`types.d.ts`（几乎每阶段都动）。
→ 所以必须**严格按阶段顺序做**，每阶段提交后再开下一阶段。

---

## 附录 B：回归测试清单（每个阶段结束都跑一遍）

```bash
# 1. Go 全量
make test

# 2. 前端构建（含 tsc 类型检查）
cd internal/site && npm run build

# 3. 关键路径手动冒烟
#    - 登录 → 系统列表（表格/卡片两种视图各看一次）
#    - 打开系统详情 → 图表正常渲染（网络/CPU/内存）
#    - 网络监控页 → 表格/卡片切换 → 点开 Sheet 看图表 → 对比功能
#    - 添加/编辑/删除一个监控
#    - 暂停/恢复一个系统
#    - 触发一次通知（把某告警阈值调到必然触发）
```

## 附录 C：常见坑速查

| 现象 | 原因 | 解法 |
|---|---|---|
| 前台拿不到新字段 | 忘了加进 `NETWORK_MONITOR_FIELDS` / `FIELDS_DEFAULT` | 搜字段名对应的常量 |
| 改配置后名称/自定义字段丢了 | 忘了加进 `copyMonitorToNewRecord` 白名单 | 同上 |
| 卡片每 10 秒闪一下 | 实时订阅没批处理 | 照 `useNetworkMonitors` 的 `pendingEvents` 模式 |
| **色条大面积显示"丢包"** | **把 `total === 0` 的块算成了 `loss`** | `getBlockLevel` 第一条必须是 `if (total === 0) return "empty"`。5 分钟探测的监控 40 分钟内只有 8 个样本，其余 12 块必须是灰 |
| 5 分钟探测的监控色条一半是灰 | 走了 2 分钟块规格 | `getBlockSpec` 在 `interval > 120` 时必须切到 `10m` 记录 / 10 分钟块 |
| 色条每 2 分钟整体跳一下 | 用 `Date.now()` 直接算块索引 | 用 `alignNow()` 对齐到块边界 |
| 卡片显示 "0 ms" | 全丢时 `resAvg1h` 为 0 被当成有效延迟 | `resAvg1h === 0 && loss1h >= 100` → 显示"丢包" |
| 流量突然暴涨到 PB 级 | 计数器重置没识别 / 网卡热插拔 | `safeDelta` + 10 TiB 保护 |
| 到期提醒每天重复 | `remindLevel` 没推进 / 比较方向反了 | 记住：`level` 越小越紧急，`-1 < 1 < 3 < 7 < 0`（0 是"无需提醒"） |
| 迁移重启后报错 | 迁移不幂等（重复 `Add` 字段） | 字段加 `Id` 并判重，或用 `RemoveById` 失败可忽略 |
| 深色模式看不清 | 硬编码颜色 | 用 `text-emerald-600 dark:text-emerald-400` |
| i18n 漏词 | 忘了 `npm run sync` | 提交前必跑 |

## 附录 D：给下一阶段 Agent 的开工提示词模板

```
你正在实现 Beszel 增强需求的【阶段 PN：<标题>】。

必读：
1. docs/PRD-beszel-extensions.md 的 §<相关章节>
2. docs/WORKPLAN-beszel-extensions.md 的【阶段 PN】与【附录 A/C】
3. docs/FOLLOWUPS.md（上一阶段遗留问题）

约束：
- 只做本阶段改动清单里列的事，不顺手重构
- 阶段结束必须 make test 全绿 + 前端 build 通过
- 提交信息以 stage-N: 开头
- 如果发现方案有问题，先在回复里说明，不要自行改设计

开始前请先读代码确认：<列出本阶段要改的文件>
```

---

## 附录 E：工期估算（仅供参考，不作为压缩依据）

| 阶段 | 相对工作量 | 说明 |
|---|---|---|
| P0 | 0.5 | 环境 |
| P1 | 1.0 | 跨栈但简单，适合建立节奏 |
| P2 | 0.5 | 一个函数 + 一个工具文件 |
| P3 | 0.5 | 4 处小改 |
| P4 | 3.5 | 前端大头：聚合纯函数 + Hook + 色块 + 卡片墙 + 切换 |
| P5 | 1.5 | 两个集合 + 纯函数 + 测试 |
| P6 | 2.0 | 对话框 + 卡片块 + 工具 |
| P7 | 1.0 | Cron + 去重 + 测试 |
| P8 | 2.0 | 统计算法 + 事务接入 + 边界测试 |
| P9 | 1.0 | Tab + 进度条 |
| P10 | 2.0 | 三个可选子项（P10.4 连接数最轻） |

**建议节奏**：P0–P2 一次会话跑完；P3–P4 一次；P5–P7 一次；P8–P9 一次。每次会话结束必须有可工作的构建。
