# Beszel 个人增强版 产品需求文档（PRD）

- 版本：v1.1
- 基线代码：Beszel v0.21.1（fork，提交 `e2ed147a`，工作区无改动）
- 目标读者：产品决策者（你自己）+ 后续执行 Agent
- 配套文档：`docs/WORKPLAN-beszel-extensions.md`（分阶段实施计划）

> **v1.1 修订记录**（依据 2026-10-24 的补充意见）
> 1. 监控卡片**取消迷你折线**，改为 Komari 风格**竖状色块条**（§6.4 整节重写）。
> 2. 明确色块的时间窗口与采样率匹配策略（20 块 × 2min，长间隔自动切 10m）。
> 3. 补齐**丢包的色块表达**（全丢红块 / 部分丢包底部红边 / 无数据灰块）。
> 4. 确认卡片延迟数字用 **`resAvg1h`（平均）** 而非最后一次探测。
> 5. 新增 §6.5 **TCP/UDP 连接数**：经核查 Agent 完全未采集，列为 P10.4 可选。
> 6. Q6 确认全局阈值、永不做 per-monitor；Q11 确认排到最后一阶段。

> 本文档的定位：**结合真实代码事实的全局方案**。所有“现状”描述都可对应到具体文件与行号；“方案”部分给出确定的数据结构、职责边界与流程，供后续 Agent 按阶段实现，不靠猜。

---

## 0. 先看结论（TL;DR）

| 需求 | 落点 | 是否改 Agent | 是否需要迁移 | 复杂度 |
|---|---|---|---|---|
| 成本配置 / 到期提醒 | 新集合 `system_billing` + 新 Cron | 否 | 是（新增集合） | 中 |
| 周期流量统计 / 重置 | 新集合 `system_traffic`（Hub 写）+ `system_billing` 存配额 | 否（复用现有网卡累计计数器） | 是 | 中高 |
| 系统卡片上下行分开显示 | `system.Info` 增加 `bu`/`bd` | 是（1 行赋值） | 否 | 低 |
| 网络监控“名称” | `network_monitors.name` | 否 | 是（加字段） | 低 |
| 网络监控 900ms 计丢包 | Agent 采样归一化 | 是 | 否 | 低 |
| 网络监控卡片视图 + 绿黄红 | 前端新组件 + 用户设置 | 否 | 否 | 中高 |

**关键架构判断**（后文展开论证）：

1. **流量统计必须由 Hub 做，Agent 只负责继续上报累计计数器。** 网卡计数器是“机器启动以来的累计值”，Hub 每 60s 采一次做逐网卡差分即可，且天然具备“Hub 离线 2 小时后恢复仍能精确补算”的性质（累计值没丢）。Agent 端不需要新增任何状态，也不需要负责重置。
2. **900ms 计丢包必须在 Agent 的样本聚合层做，不能只在前端改颜色。** 否则 `loss1h`、历史 `network_monitor_stats`、丢包告警、图表口径会不一致。
3. **成本与流量配置不能塞进 `systems` 集合。** `systems` 是实时订阅对象（`systemsManager.ts` 里 `FIELDS_DEFAULT = "id,name,host,port,info,status"` + realtime subscribe）。流量运行态每分钟写一次，若写进 `systems` 会造成全体客户端每分钟被动刷新、且把 60s 的 `info` 更新与计费更新耦合成同一次事务提交。
4. **网络监控名称绝对安全**：`generateMonitorID` 只用 `systemId + target + protocol (+port/+server)`，`name` 不参与，改名称不会让 Agent 重建监控、不会丢历史。

---

## 1. 背景与目标

### 1.1 背景

当前 Beszel 是一个 Hub（PocketBase）+ Agent（Go）的监控系统：

- **Hub**：PocketBase Web 应用（`internal/hub`），负责持久化、UI（`internal/site`）、通知（`internal/alerts`）、Cron（`internal/hub/hub.go`）。
- **Agent**：装在被监控机器上（`agent/`），通过 WebSocket（CBOR）或 SSH 回传数据。
- 系统列表卡片网格：`grid-cols-1 sm:grid-cols-2 lg:grid-cols-3`（`systems-table.tsx:402`）。

三项需求的共同点是：**Komari 已有的“机器资产生命周期”视角**（这台机器值多少钱、什么时候到期、这个月流量用完了没、网络质量怎么样），而 Beszel 目前只做“运行时可观测性”。

### 1.2 目标

| 编号 | 目标 | 可衡量结果 |
|---|---|---|
| G1 | 在系统卡片上一眼看到“这台机器花多少钱、还有多久到期” | 卡片出现价格/周期/剩余天数；到期前 7 天收到通知 |
| G2 | 在系统卡片上一眼看到“这个周期流量用了多少、还有多久重置” | 卡片出现已用/总量/剩余/重置倒计时 |
| G3 | 网络延迟监控从“表格”升级为“按机器聚合的卡片墙” | 全局监控页可切换卡片视图；一台机器一张卡，卡内最多 3 个监控块（Komari 风格竖状色块，不做迷你折线） |
| G4 | 延迟质量可视化统一口径 | 绿/黄/红三色；丢包率与“≥900ms 视为丢包”一致 |
| G5 | 监控有可读名称 | 显示“上海电信”而不是 `1.2.3.4:443` |
| G6 | 系统卡片网络值分方向 | 同时显示 ↑ / ↓ 速率 |

### 1.3 非目标（明确不做）

- 不做账单/发票/多币种汇率换算（不做历史汇率、不做汇总折算）。
- 不做“续费自动提醒后自动记账”的账本功能。
- 不做 Agent 侧的流量计费（Agent 保持无状态）。
- 不改造现有告警的 `alerts` 记录模型（除非明确进入可选的阶段 10）。
- 不做多租户计费隔离（个人使用，所有系统成员可见计费信息）。

---

## 2. 现状事实清单（代码级）

> 这一节是后续 Agent 的“事实基线”，避免重复考古。

### 2.1 数据与集合

| 集合 | 关键字段 | 说明 |
|---|---|---|
| `systems` | `name,status,host,port,info(JSON),users(relation[])` | `info` 存 `system.Info`；`users` 决定可见性 |
| `system_stats` | `system,stats(JSON),type` | `type` ∈ `1m/10m/20m/120m/480m` |
| `system_details` | 系统静态详情 | — |
| `network_monitors` | `system,target,protocol,port,interval,server,skipTlsVerify,res,resAvg1h,resMin1h,resMax1h,loss1h,enabled,certInfo` | **无 `name` 字段** |
| `network_monitor_stats` | `monitor,system,type,total_count,success_count,res_sum,res_min,res_max` | 原始计数，前端算 avg/loss |
| `alerts` | `user,system,name(select),value,min,triggered,state(隐藏JSON),pending_since` | `name` 是**固定 select 枚举**，新增枚举值需迁移 |
| `alerts_history` | 告警历史 | — |
| `user_settings` | `user,settings(JSON,2MB)` | **新增前端设置项不需要迁移**（JSON blob） |
| `quiet_hours` | 免打扰时段 | `SendAlert` 会检查 |

集合权限集中在 `internal/hub/collections.go`：

```go
systemsMemberRule   = `@request.auth.id != "" && users.id ?= @request.auth.id`
systemMemberRule    = `@request.auth.id != "" && system.users.id ?= @request.auth.id`
systemsWriteRule    = systemsReadRule + ` && @request.auth.role != "readonly"`
systemScopedWriteRule = systemScopedReadRule + ` && @request.auth.role != "readonly"`
```

`SHARE_ALL_SYSTEMS=true` 时读规则放宽为“任意登录用户”。

### 2.2 系统更新与采样节拍

`internal/hub/systems/system_manager.go`：`interval = 60_000`（毫秒）。
`internal/hub/systems/system.go:162 update()`：

```
paused → handlePaused() 直接 return（不拉数据、不写 records）
否则 fetchDataFromAgent(CacheTimeMs: 60_000) → sys.data = data → createRecords(data)
```

`createRecords()`（`system.go:258`）在一个事务里写 `system_stats`、`container_stats`、`system_details`、网络监控结果。**这是流量累计最合适的挂载点**（与 `system_stats` 同事务，保证“统计到的一分钟”和“流量加的一分钟”一致）。

`createRecords` 末尾（`system.go:342`）执行 `systemRecord.Set("info", info)` → 说明 `Info` 结构新增字段会自动落库到 `systems.info`。

### 2.3 网络计数器（流量统计的数据源）

`agent/network.go:199`：

```go
systemStats.NetworkInterfaces[v.Name] = [4]uint64{upDelta, downDelta, v.BytesSent, v.BytesRecv}
//                                          [ 速率,    速率,   累计发送,  累计接收 ]
```

- `[2]` / `[3]` 是 `psutilNet.IOCounters(true)` 的**机器启动以来累计字节数**。
- `[0]` / `[1]` 是 Agent 自己按 `msElapsed` 算出的**瞬时速率**（字节/秒）。
- 网卡过滤在 `skipNetworkInterface`：默认排除 `lo*/docker*/br-*/veth*/bond*/cali*` 以及收发为 0 的网卡；支持 `NICS` 环境变量白名单/黑名单。
- `Stats.Bandwidth [2]uint64` = `[发送速率, 接收速率]`，单位字节/秒（`applyNetworkTotals` 赋值）。

> **重要**：`NetworkInterfaces` 只包含“被采纳”的网卡。如果用户在 Agent 端用 `NICS` 只保留 `eth0`，Hub 侧统计就只算 `eth0`，这正是想要的行为。

### 2.4 为什么卡片只显示一个网络值

`agent/system.go:272`：

```go
a.systemInfo.BandwidthBytes = systemStats.Bandwidth[0] + systemStats.Bandwidth[1]
```

`system.Info` 只有 `BandwidthBytes uint64 json:"bb"`（`entities/system/system.go:180`）。
前端网络列（`systems-table-columns.tsx`）读的是 `info.bb` → 只能显示合并值。

`Info` 的 cbor key 已用到 27，**28/29 可用**。

### 2.5 网络监控的 ID 与配置同步

- `internal/hub/network_monitors.go::generateMonitorID`：`systemId + target + protocol (+port) (+server)` 做 hash → `name` 不参与。
- `copyMonitorToNewRecord` 复制字段白名单：`system,target,protocol,port,server,interval,enabled,skipTlsVerify` → **新增 `name` 必须加入**，否则配置变更触发 ID 重建时会丢名称。
- `monitorConfigFromRecord` 只挑 Agent 需要的字段 → `name` 不需要发给 Agent。
- 版本门槛：`beszel.MinVersionNetworkMonitors = 0.20.0`（`beszel.go`）。

### 2.6 网络监控探针与丢包口径

`agent/network_monitor_probe.go`：TCP/DNS 超时 `3s`，HTTP 最大 `10s`（`monitor.MaxProbeTimeout`）。失败统一 `return -1, err`。

`agent/network_monitor_task.go:96-105`（**唯一收敛点**）：

```go
responseUs, err := probe(task.ctx, task.config)
...
if err != nil { responseUs = -1; ...log... }
result := task.history.record(monitorSample{responseUs: responseUs, timestamp: now})
```

`agent/network_monitor_history.go:94`：

```go
func (agg *monitorAggregate) addResponse(responseUs int64) {
	agg.totalCount++
	if responseUs < 0 { return }        // ← 唯一的“丢包”定义
	agg.successCount++
	agg.sumUs += responseUs
	...
}
```

丢包率 = `(total - success) / total`（`lossPercentage`），既用于 1m 结果也用于 1h 桶。**所以“900ms 计丢包”只要在 `record()` 之前把 `responseUs` 归一化成 `-1`，全链路自动一致**（`loss1h`、`network_monitor_stats`、丢包告警、图表）。

### 2.7 前端现状

| 关注点 | 文件 | 事实 |
|---|---|---|
| 系统卡片网格 | `systems-table.tsx:402` | `grid-cols-1 sm:grid-cols-2 lg:grid-cols-3` |
| 视图切换 | `systems-table.tsx:174-180` | `viewMode` 持久化：`$userSettings.viewMode` → `localStorage["besz-viewMode"]` → 默认 |
| 卡片操作菜单 | `systems-table-columns.tsx:727 ActionsButton` | 编辑 / 暂停恢复 / 复制名称 / 复制 Host / 删除 |
| 监控表 | `network-monitors-table.tsx` | 只有表格；行点击开 `NetworkMonitorSheet` 看图表 |
| 监控阈值 | `network-monitors-columns.tsx` | 硬编码 `responseTimeThresholds`（http 800ms/3s、tcp 500ms/2s、icmp 100ms/500ms、dns 150ms/800ms） |
| 监控字段订阅 | `use-network-monitors.ts:85 NETWORK_MONITOR_FIELDS` | 新增 `name` 必须加入 |
| 监控显示名 | `monitor-compare.ts::getMonitorTarget` | 只按协议格式化 target，无名称概念 |
| 监控图表 | `routes/system/charts/monitors-charts.tsx` | `ResponseChart` / `LossChart` / `AvgMinMaxResponseChart`，基于 Recharts `LineChartDefault` |
| 监控统计 Hook | `use-network-monitors.ts::useNetworkMonitorStats` | 按 monitorId 集合拉 `network_monitor_stats`，带内存 cache + realtime 订阅 |
| 用户设置 | `user_settings.settings`(JSON) + `api.ts::queueUserSettings` | 新增 `monitorViewMode` 等**无需迁移** |

---

## 3. 全局设计：职责边界

```
┌──────────────────────── Agent（无状态） ────────────────────────┐
│ 采集：CPU/内存/磁盘/网卡累计计数器/监控探测样本                  │
│ 新增：单调递增的网卡累计值已在 NetworkInterfaces[2][3] 里（已有） │
│ 新增：探测样本 ≥ 900ms → 归一化为丢包（本需求唯一 Agent 改动）    │
│ 不负责：计费、配额、重置、累计存储                                │
└────────────────────────────────────────────────────────────────┘
                                  │ CBOR over WS / SSH（60s）
                                  ▼
┌──────────────────────── Hub（有状态） ──────────────────────────┐
│ createRecords() 事务：                                          │
│   ├─ system_stats（已有）                                       │
│   ├─ 流量差分累计 → system_traffic（新增，同事务）               │
│   └─ systems.info（已有，新增 bu/bd 方向字段）                  │
│ Cron：                                                          │
│   ├─ 流量周期滚动/手动重置（每个采样点顺带判断，无需独立 Cron）   │
│   └─ 每日一次：到期提醒 + 流量阈值提醒（新增）                   │
│ 通知：alerts.SendAlert（复用邮件/Webhook/免打扰）               │
└────────────────────────────────────────────────────────────────┘
                                  │ REST + Realtime
                                  ▼
┌──────────────────────── 前端 ───────────────────────────────────┐
│ 系统卡片：价格/周期/剩余天数 + 流量进度条 + ↑↓ 速率              │
│ 横点菜单：计费与流量对话框（Tabs：费用 / 流量）                  │
│ 监控页：表格 ⇄ 卡片墙，卡片内最多 3 个绿黄红色块条                │
└────────────────────────────────────────────────────────────────┘
```

### 3.1 为什么流量不放 Agent

| 方案 | 问题 |
|---|---|
| Agent 累计并上报“本周期已用” | Agent 需要知道周期边界、配额、重置逻辑；Agent 重启/升级/换机即丢状态；用户改重置日要重新下发配置；多 Hub 场景会重复计费 |
| Hub 累计（选中） | 数据源是单调累计计数器，天然抗重启（机器重启才归零，可识别）；Hub 停机恢复后能精确补算；配置全在 Hub，前端改完立即生效；Agent 无需版本升级 |

唯一代价：**机器重启会丢掉“上次采样 → 重启”之间那不到 60s 的量**。个人使用可接受，且在文档中明确写出。

---

## 4. 功能一：成本统计与到期提醒

### 4.1 用户故事

> 我有一台 ¥25/月的 VPS，2026-03-14 到期。我希望在系统卡片上看到「¥25/月 · 剩余 12 天」，并在到期前 7 天收到一封邮件/Webhook，提醒我续费。

### 4.2 数据结构：`system_billing` 集合

1:1 于 `systems`（`system` 字段唯一索引），**同时承载成本与流量配额配置**（同属“计费元数据”，一次查询、一次权限配置搞定；卡片只需额外拉一次）。

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `id` | text(15) PK | 自动 | — |
| `system` | relation(`systems`, maxSelect 1, cascadeDelete) | 必填 | 唯一索引 `idx_billing_system` |
| **成本组** | | | |
| `billingType` | select `free` \| `paid` | `paid` | `free` 时隐藏价格/周期/到期 |
| `price` | number | 0 | 允许 2 位小数（金额） |
| `currency` | select `CNY` \| `USD` \| `EUR` \| `GBP` | `CNY` | 展示符号：¥ / $ / € / £ |
| `cycle` | select `monthly` \| `semiannual` \| `annual` \| `biennial` \| `triennial` \| `permanent` | `monthly` | `permanent` = 永久（无到期日） |
| `paidAt` | date | 空 | 本次付款日（可选，用于“按周期推算到期日”按钮） |
| `expiresAt` | date | 空 | **到期日的唯一真相**（`permanent`/`free` 时为 0） |
| `notifyDaysBefore` | number(int, 0–60) | 7 | 到期前多少天开始提醒 |
| `remindLevel` | number(int) | 0 | **去重状态**：已提醒到的档位（7/3/1/0/-1） |
| `remindAt` | date | 空 | 最近一次提醒时间（展示与排障用） |
| **流量组**（见第 5 章） | | | `trafficLimit`、`trafficMode`、`resetDay`、`trafficAlerts`、`trafficRemindLevel` |
| `created` / `updated` | autodate | — | — |

**为什么 `expiresAt` 是真相而不是 `paidAt + cycle` 自动算？**
因为现实里存在“按月付款但实际到期日改过”“赠送期”“延期一周”等情况。UI 提供「按周期自动推算」按钮（`paidAt + cycle` → 填 `expiresAt`），但存储以 `expiresAt` 为准，避免每次渲染都做日历运算+时区歧义。

### 4.3 交互

**入口 A：系统卡片右侧三个横点 → 「计费与流量」**（`ActionsButton` 新增菜单项）
→ 打开 `BillingDialog`（Tabs：`费用` / `流量`）
→ readonly 用户只能看，输入框 `disabled`。

**入口 B（可选，阶段 6 一起做）：系统详情页头部**显示一条 `Badge`：`¥25/月 · 12 天`，点击同样打开对话框。

**费用 Tab 表单**：

```
计费类型   ( ) 付费   ( ) 免费
价格       [ 25.00 ]  货币 [ CNY ▾ ]
付款周期   [ 每月 ▾ ]
付款日期   [ 2026-02-14 ]   [按周期推算到期日]
到期日期   [ 2026-03-14 ]
提前提醒   [ 7 ] 天
─────────────────────────────
预览：¥25/月 · 剩余 12 天（2026-03-14 到期）
```

校验：
- `billingType=paid` 且 `cycle≠permanent` 时 `expiresAt` 必填，且必须 > `paidAt`（若填了 `paidAt`）。
- `price >= 0`；`cycle=permanent` 时 `expiresAt` 清空。
- `billingType=free` 时 `price/cycle/expiresAt` 全部清空，卡片显示「免费」。

### 4.4 卡片展示规则

在 `SystemCard` 底部追加一行（`grid` 视图），表格视图追加一个可选列（默认隐藏，列名 `Billing`）：

| 状态 | 展示 |
|---|---|
| 无 `system_billing` 记录 | 不显示任何计费信息（不占位） |
| `free` | `免费`（次要色） |
| `permanent` | `¥25 · 永久` |
| 未到期 | `¥25/月 · 剩余 12 天`；剩余 ≥ 30 天用次要色，7–29 天黄色，<7 天红色 |
| 已到期 | `已过期 3 天`（红色） |
| 流量（若开启） | 进度条 + `已用 812 GB / 1 TB · 18 天 后重置` |

金额格式化：整数不显示小数（`¥25`），有小数保留 2 位（`¥25.50`）。周期短标签：`/月`、`/半年`、`/年`、`/2年`、`/3年`、`永久`。

### 4.5 到期提醒流程

**Cron**：`30 0 * * *`（UTC 00:30 = 北京 08:30），注册在 `internal/hub/hub.go::registerCronJobs`。

**伪代码**：

```
for each billing in system_billing where billingType=paid && cycle!=permanent && expiresAt>0:
    daysLeft = ceil((expiresAt 的当地 00:00 - today 00:00) / 1day)   # 按整天算
    level    = 当前命中的档位:
               daysLeft <= 0        → -1  (已到期，已过期 N 天)
               daysLeft <= 1        → 1
               daysLeft <= 3        → 3
               daysLeft <= notifyDaysBefore → notifyDaysBefore(默认7)
               否则                  → 0 (无需提醒)
    if level == 0 || level >= billing.remindLevel:   # 已提醒过同档或更早档
        continue
    系统名 = systems.name
    金额   = format(price, currency)
    for each user in systems.users:
        SendAlert({
            UserID: user,
            SystemID: system,
            Title: "机器即将到期：{系统名}",
            Message: "{系统名}（{金额}/{周期}）将于 {日期} 到期，剩余 {N} 天。",
            Link:   "{HUB_URL}/system/{id}",
            LinkText: "查看系统",
        })
    billing.remindLevel = level; billing.remindAt = now; save
```

**关键点**：

1. **档位单调推进**：`remindLevel` 记录“已提醒到的最紧急档位”。因为 `level` 数值越小越紧急（7 → 3 → 1 → -1），用 `level >= remindLevel` 判断“已经提醒过这个档或更宽松的档”。续费/改期后（`expiresAt` 变大导致 `level` 回到 7 档之外）需要**重置** `remindLevel=0`，靠 `expiresAt` 变化检测（在 `system_billing` 的 update hook 里：`expiresAt:changed` → `remindLevel=0`）。
2. **不是每日轰炸**：同一档位只发一次。若 `notifyDaysBefore=7`，则第 7 天发一次、第 3 天一次、第 1 天一次、到期当天一次，共 4 次。
3. **收件人 = 系统成员**：与 Beszel 现有“告警按 user+system 归属”一致。`SendAlert` 内部会再次校验 `userHasSystem` 与免打扰时段，因此不需要在 Cron 里重复处理 `quiet_hours`（免打扰命中会静默丢弃；对“到期提醒”这种低频通知可接受，若要“免打扰也必达”需在 `SendAlert` 增加 `IgnoreQuietHours` 开关 —— 列为开放问题 Q3）。
4. **不用 `alerts` 集合**：`alerts.name` 是固定 select 枚举，且语义是“阈值触发型告警（有 triggered 状态机）”。到期提醒是“日期型、一次性、无恢复概念”，塞进去会产生无意义的 `triggered=true` 常驻记录。**直接调 `SendAlert` 复用通知渠道即可**（`SendAlert` 与 alerts 记录完全解耦，见 `alerts.go:214`）。
5. **改期/续费**：用户在 `BillingDialog` 里把 `expiresAt` 往后改 → hook 重置 `remindLevel=0` → 下一轮 Cron 重新走档位。

### 4.6 边界与异常

| 场景 | 行为 |
|---|---|
| `expiresAt` 为空 | 不提醒，卡片只显示价格/周期 |
| 已到期且 `remindLevel` 已为 -1 | 不再提醒（避免永久刷屏），卡片持续红色显示 |
| 系统被删除 | `system_billing.system` 级联删除 |
| 系统暂停 | 仍然提醒（暂停只是停止抓数据，不代表不续费） |
| 用户没有配置任何通知渠道 | `SendAlert` 静默返回（`len(Emails)==0` 且 `len(Webhooks)==0` → `return nil`）。日志会记录，不报错 |
| Hub 当天 00:30 停机 | Cron 错过；PocketBase Cron 不做补跑，第二天会按新 `daysLeft` 重新计算档位（可能跳档，如从 7 直接到 5，仍会提醒一次）。可接受 |
| 时区 | 统一用 **Hub 进程本地时区**（`time.Local`）。README 里注明：容器部署请设置 `TZ=Asia/Shanghai`。列出为开放问题 Q4 |

---

## 5. 功能二：周期流量统计与重置

### 5.1 用户故事

> 我这台机器每月 14 日重置，1 TB 双向流量。我想在卡片上看到「已用 812 GB / 1 TB，18 天后重置」，并且能设「用到 80% 提醒我」。

### 5.2 数据模型

#### 5.2.1 配置（在 `system_billing` 里，与成本同表）

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `trafficLimit` | number(int64) | 0 | 上限字节数；**0 = 不限流量**（关闭统计展示） |
| `trafficMode` | select `sum` \| `upload` | `sum` | `sum`=上下行合计计费；`upload`=仅上行计费 |
| `resetDay` | number(int, 1–31) | 1 | 每月几号重置；>当月天数时取当月最后一天 |
| `trafficAlerts` | bool | true | 是否启用 80%/100% 提醒 |
| `trafficRemindLevel` | number(int) | 0 | 去重：0=未提醒 / 80 / 100 / -1 |
| `manualResetAt` | date | 空 | 用户点「立即重置」写当前时间；Hub 采样时发现它比 `system_traffic.lastResetAt` 新则执行重置 |

#### 5.2.2 运行态 `system_traffic` 集合（**Hub 独占写入**）

| 字段 | 类型 | 说明 |
|---|---|---|
| `system` | relation maxSelect 1，唯一索引 | 1:1 |
| `periodStart` | date | 本周期起点（含） |
| `periodEnd` | date | 下个重置点（不含），前端据此算倒计时 |
| `usedUpload` | number(int64) | 本周期已用上行字节（**始终统计**） |
| `usedDownload` | number(int64) | 本周期已用下行字节（`upload` 模式只展示不计费） |
| `lastCounters` | json | `{"eth0":[sentTotal,recvTotal], "ens3":[...]}` 上一采样快照 |
| `lastSampleAt` | date | 上次成功累计时间（前端可判断“数据新鲜度”） |
| `lastResetAt` | date | 上次重置时间 |
| `resetReason` | select `auto`\|`manual`\|`init` | 便于排障 |

权限：读 = `systemScopedReadRule`（成员可看）；**create/update/delete = nil**（禁止 API 写入，只有 Hub 服务端 superuser 上下文可写）。手动重置通过 `system_billing.manualResetAt` 间接触发，不需要自定义路由。

### 5.3 统计算法

每个采样点（`createRecords` 事务内，紧跟 `system_stats` 写入之后）：

```
func accumulateTraffic(systemRecord, stats, billing, traffic):
    if billing == nil || billing.trafficLimit == 0: return        # 未启用

    now = stats 采样时间（用 time.Now().UTC()）
    # 1) 周期滚动
    if traffic.periodEnd == 0 or now >= traffic.periodEnd or billing.manualResetAt > traffic.lastResetAt:
        traffic.periodStart = 本周期起点(now, billing.resetDay)
        traffic.periodEnd   = 下个重置点(now, billing.resetDay)
        traffic.usedUpload = traffic.usedDownload = 0
        traffic.lastResetAt = now
        traffic.resetReason = manual? auto
        traffic.lastCounters = {}          # 清空，避免跨周期差分
        # 注意：本采样点仍要建立 baseline，见步骤 2 的“首次”分支

    # 2) 逐网卡差分
    upDelta, downDelta = 0, 0
    for name, v in stats.NetworkInterfaces:
        s, r = v[2], v[3]                  # 累计发送 / 累计接收
        prev = traffic.lastCounters[name]
        if prev == nil:
            # 首次见到该网卡（新装/新网卡/周期重置）：只建立基线，不加量
            traffic.lastCounters[name] = [s, r]
            continue
        dUp  = diff(prev[0], s)
        dDown= diff(prev[1], r)
        upDelta += dUp; downDelta += dDown
        traffic.lastCounters[name] = [s, r]

    traffic.usedUpload   += upDelta
    traffic.usedDownload += downDelta
    traffic.lastSampleAt  = now
    save(traffic)

# 差值函数：处理计数器回绕/机器重启
func diff(prev, cur):
    if cur >= prev: 
        d = cur - prev
        if d > 10 * TB: return 0            # 异常值保护（网卡热插拔/驱动 bug）
        return d
    else:
        # 计数器归零（机器重启）：把当前值当作“重启后已用量”
        return cur
```

**为什么这样设计（必须写进代码注释）**：

- **不改 Agent**：数据源已经存在（`NetworkInterfaces[2][3]`）。
- **抗 Hub 长时间离线**：累计计数器不因 Hub 不在而停止增长。Hub 恢复后一次 diff 就能把整段离线时间的量补上（只要机器没重启）。这是本方案相比“Agent 上报增量”的最大优势。
- **抗 Agent 重启**：Agent 重启不影响 OS 网卡计数器。
- **抗机器重启**：`cur < prev` → 认为归零，用 `cur` 作为增量。损失“上次采样到重启”之间的量（< 60s），并可能把“重启前的少量残留”误算，量级可忽略。
- **抗网卡增减**：`lastCounters` 是 map，新网卡走“首次建立基线”，旧网卡消失只是不再贡献增量（与 `agent/network.go::sumAndTrackPerNicDeltas` 同思路，避免“合计值相减”在网卡消失时下溢）。
- **10 TB 保护**：单次差值超过 10 TB 直接丢弃并记 warning 日志（正常 60s 内不可能），防止驱动 bug 导致爆量。

### 5.4 周期边界计算

```
func 本周期起点(now, resetDay):
    y, m, _ = now.Date()
    d = min(resetDay, 当月最后一天(y, m))
    candidate = Date(y, m, d, 00:00, 时区)
    if now >= candidate: return candidate
    # 否则取上一个月的重置点
    y2, m2 = 上一月(y, m)
    d2 = min(resetDay, 当月最后一天(y2, m2))
    return Date(y2, m2, d2, 00:00, 时区)
```

- 时区：**Hub 进程本地时区**（`time.Local`）。
- `resetDay=31`：2 月取 28/29，4 月取 30。**不做“顺延到 3/1”**（会造成周期重叠），统一“该月最后一个自然日”。
- `periodEnd` = 下个重置点（同算法取下一个）。

### 5.5 交互

**入口**：卡片横点 →「计费与流量」→ `流量` Tab。

```
流量上限   [ 1 ] [TB ▾]        （0 或留空 = 不限）
计费方式   ( ) 上下行合计   ( ) 仅计上行
重置日     每月 [ 14 ] 日
阈值提醒   [x] 用量达 80% 时提醒
──────────────────────────────
本周期：3 月 14 日 – 4 月 14 日
已用   812 GB / 1 TB   (79%)
上行   402 GB   下行 410 GB
剩余   18 天 6 小时
[ 立即重置 ]
```

- 单位选择：`MB / GB / TB / PB`，内部统一存字节（1 GB = 1024³）。
- 「立即重置」→ 二次确认 → `system_billing.manualResetAt = now` → 下一个采样点（≤60s）由 Hub 执行重置。
- 未启用流量（`trafficLimit=0`）时，Tab 内只显示表单，卡片不显示流量区。

### 5.6 卡片展示

在系统卡片计费行下方（有流量配置时才出现）：

```
[██████████████░░░░░░]  812 GB / 1 TB · 18 天 后重置
```

- 进度条颜色：<80% 正常色，80–99% 黄色，≥100% 红色。
- 「剩余量」= max(0, limit - 计费用量)。`upload` 模式下「已用」按 `usedUpload` 计，但下方小字仍显示上下行拆分。
- 点击进度条 → 打开 `流量` Tab（或后续做一个流量历史 Mini 图）。

### 5.7 边界与异常

| 场景 | 行为 |
|---|---|
| 系统暂停（`paused`） | **不计流量**（`update()` 直接 return，拿不到样本）。卡片灰显并提示「已暂停，暂停期间不计流量」 |
| Agent 离线 / 系统 `down` | 不计流量（无样本）。恢复后靠累计计数器一次性补齐（机器未重启） |
| Hub 停机 2 小时 | 恢复后一次 diff 补齐 2 小时量（精确） |
| 机器重启 | 该网卡本次差分为「归零补偿」，损失 < 60s 的量 |
| 用户改 `resetDay` | 立即按新规则重算 `periodEnd`；**不重置已用量**（当前周期继续，到新的 `periodEnd` 才归零）。避免用户误操作清空数据 |
| 用户改 `trafficLimit` | 立即生效，前端百分比重算 |
| 用户改 `trafficMode` | 立即生效（`upload` 模式下已用量口径变为 `usedUpload`，历史量不重算） |
| 新增网卡 / 换网卡 | 首次采样只建基线，不加量 |
| `trafficLimit` 设为 0 | 停止累计（`system_traffic` 保留旧值不清空，便于恢复） |
| 系统删除 | `system_traffic.system` 级联删除 |

### 5.8 流量阈值提醒（阶段 9，可选）

复用第 4.5 节的**同一个 Cron**：

```
usage = (trafficMode==upload ? usedUpload : usedUpload+usedDownload)
pct   = usage * 100 / trafficLimit
level = pct >= 100 ? -1 : pct >= 80 ? 80 : 0
if level == 0 || level >= trafficRemindLevel: continue
for user in systems.users: SendAlert(...)
trafficRemindLevel = level
```

- 周期滚动时把 `trafficRemindLevel` 清零。
- 同一周期内 80% 提醒一次、100% 提醒一次。

---

## 6. 功能三：网络延迟监控升级

### 6.1 3.1 监控名称（需求优先级最高，改动最小）

**数据**：`network_monitors` 新增 `name`（Text，max 100，非必填，默认空）。

**影响面清单（缺一不可）**：

| 位置 | 改动 |
|---|---|
| `internal/migrations/<ts>_network_monitor_name.go` | `collection.Fields.Add(&core.TextField{Id:"nm_name", Name:"name", Max:100})` |
| `internal/hub/network_monitors.go::copyMonitorToNewRecord` | 字段白名单加 `"name"` |
| `use-network-monitors.ts::NETWORK_MONITOR_FIELDS` | 加 `name` |
| `types.d.ts::NetworkMonitorRecord` | 加 `name?: string` |
| `lib/monitor-compare.ts` | 新增 `getMonitorDisplayName(m) = m.name?.trim() \|\| getMonitorTarget(m)` |
| `network-monitors-columns.tsx` 目标列 | 主行显示名称，副行小字显示 target |
| `network-monitors-table.tsx` Sheet 标题 / globalFilter 搜索串 | 用显示名 + target 一起参与搜索 |
| `monitors-charts.tsx::MonitorChart` 的 `getLabel` 默认值 | 改为 `getMonitorDisplayName` |
| `monitor-dialog.tsx` | 表单加「名称」字段 + Zod schema + `buildMonitorPayload` 带上 `name` |
| 批量导入（若支持 CSV） | 允许有/无 `name` 列，缺列时 `name=""` |

> `monitorConfigFromRecord` **不改**：Agent 不需要名称。

**展示规则**：`name` 为空 → 完全退回现有行为（显示 target）；`name` 有值 → 名称加粗为主信息，target 作为副标题（11px，`text-muted-foreground`）。

### 6.2 3.2 900ms 以上视为丢包

**常量与环境变量**：

```go
// agent/network_monitor_task.go（或新建 agent/network_monitor_thresholds.go）
// monitorLossThresholdUs: 响应时间达到该阈值即视为丢包（与 Komari 对齐）。
// 通过 MONITOR_LOSS_THRESHOLD_MS 覆盖，默认 900ms；<= 0 表示关闭。
```

**实现点**（唯一收敛点 `network_monitor_task.go:96-105`）：

```go
responseUs, err := probe(task.ctx, task.config)
slow := false
if err != nil {
    responseUs = -1
    ... 现有失败日志 ...
} else if lossThresholdUs > 0 && responseUs >= lossThresholdUs {
    slow = true
    responseUs = -1                    // ← 归一化：全链路当作丢包
    ... 与失败同样的限频日志，但文案区分 "monitor slow (treated as loss)" ...
}
```

**为什么归一化而不是“另加一个 slow 字段”**：

- `addResponse` 里 `responseUs < 0` 是唯一的丢包定义，归一化后：`PacketLoss`、`PacketLoss1h`、`total_count/success_count`、`res_sum/min/max`、`network_monitor_stats` 历史、丢包告警阈值、Recharts 丢包图 —— **全部自动一致**。
- 若改成“只在 UI 里把 >900ms 画成丢包”，会出现：图表丢包率 0%、告警不触发、但界面显示丢包 → 自相矛盾。

**副作用与需要同步测试的点**：

1. 丢包告警（`alerts_network_monitors.go::alertNameNetworkMonitorLoss`）触发概率上升 —— 这是**期望行为**。
2. `AvgResponse1h` 会因剔除慢样本而**下降**（更快），这是符合“慢 = 丢”的语义的。
3. 红色区间变成 `crit ≤ res < 900ms`（默认 `crit=400ms`）。
4. Agent 版本：不涉及 CBOR 字段变化，Hub 无需版本门槛。但**旧 Agent 不会归一化**，因此“900ms 计丢包”只在新 Agent 上生效 —— README 需说明。

### 6.3 3.3 绿/黄/红阈值

**设计决定**：两套阈值，职责不同。

| 阈值 | 归属 | 默认 | 可配置 | 作用 |
|---|---|---|---|---|
| 丢包阈值 | **Agent** | 900ms | `MONITOR_LOSS_THRESHOLD_MS`（Agent 环境变量，`<=0` 关闭） | 决定“是不是丢包”，影响统计数据 |
| 展示阈值 | **前端** | warn 150ms / crit 400ms | 用户设置 `monitorWarnMs` / `monitorCritMs`（设置页 → General） | 只决定颜色，不影响数据 |

理由：丢包阈值改的是**数据语义**，必须由采集端统一（否则不同 Agent 数据不可比）；展示阈值是**个人偏好**，放前端不产生迁移与重启成本。

**用户设置结构（`user_settings.settings` JSON，无需迁移）**：

沿用 `colorWarn` / `colorCrit` 的**扁平键**约定（设置表单用 `FormData` 提交，值为字符串，读取端统一 `parseInt` + 夹取）：

```ts
monitorWarnMs?: number   // default 150
monitorCritMs?: number   // default 400（若 <= warnMs，自动抬到 warnMs + 1）
```

**颜色映射（统一函数 `lib/monitor-thresholds.ts`）**：

```ts
export function getMonitorLevel(resUs: number | null | undefined, t = DEFAULT_MONITOR_THRESHOLDS): MonitorLevel {
  if (resUs == null || resUs < 0) return "loss"      // 丢包（Agent 把失败与超阈值都记为 -1）
  if (resUs <= t.warnMs * 1000) return "good"        // 绿
  if (resUs <= t.critMs * 1000) return "warn"        // 黄
  return "crit"                                      // 红（<900ms，因为 ≥900 已被 Agent 计为丢包）
}
```

> 该文件还导出 `monitorLevelBg` / `monitorLevelText` 两张颜色表 + `getMonitorThresholds(settings)`（负责字符串强转、非法值回落、`crit > warn` 约束），表格与 P4 色块共用，避免两处配色漂移。

颜色取值（Tailwind 语义类，跟随主题明暗；**复用项目已有的 green/yellow/red**，与 `certExpiryDotColors`、`SYSTEM_STATUS_COLORS` 保持一致）：

| level | 文本 | 色块背景 | 说明 |
|---|---|---|---|
| good | （默认色） | `bg-green-500` | ≤150ms |
| warn | `text-yellow-600 dark:text-yellow-500` | `bg-yellow-500` | 150–400ms |
| crit | `text-red-500` | `bg-red-500` | 400–900ms |
| loss | `text-red-600 dark:text-red-400` | `bg-red-600` | 全丢 / 丢包 ≥50% / 超时 / 错误 / ≥900ms |
| empty | `text-muted-foreground` | `bg-muted` | 该时段无探测样本（**不是丢包**） |

**替换现有硬编码**：`network-monitors-columns.tsx::responseTimeThresholds` 的按协议阈值**已删除**（P2 完成）。**Q6 已定：统一全局 150/400ms，不做 per-monitor 覆盖，后续也不需要**。

> 注意对比度：`crit` 用 `bg-red-500`、`loss` 用 `bg-red-600`。两者深浅不同但同属红色系，因为“慢到红”和“丢了”对用户是同一个动作（去详情页看）。若调成两种色相会破坏“绿黄红”的直觉。

### 6.4 3.4 卡片视图（核心交互）

> **v1.1 修订**：卡片内**不再画迷你折线**，改为 Komari 风格的**竖状色块条**（quality blocks）。折线等精确图表只在点击卡片后的 Sheet 里看。理由：卡片要“一眼判断好坏”，折线在小尺寸下既看不清趋势又消耗性能（50 个监控 × Recharts 会明显卡顿）。

#### 6.4.1 视觉规格（对齐 Komari 截图）

```
┌────────────────────────────────────┐
│ 上海电信                      TCP │  ← 名称（无名称则 target）+ 协议标签
│ 延迟            168 ms             │  ← 1 小时平均延迟，按等级着色
│ 丢包率            15%              │  ← 1 小时丢包率，>0 时红色
│ ▮▮▮▮▮▮▮▮▮▮▮▮▮▮▮▮▮▮▮▮             │  ← 20 个竖状色块
└────────────────────────────────────┘
```

- 色块：`h-3`（12px）圆角 1px，宽度 `flex-1` 均分，块间 `gap-[2px]`。数量固定 20，宽度自适应容器。
- 卡片内边距 `p-2.5`，`rounded-lg border`，hover 时 `bg-accent/50`。
- 整块可点击 → 打开现有 `NetworkMonitorSheet`。

#### 6.4.2 布局与数量上限

**页面级**（系统卡片墙）：

```tsx
<div className="grid gap-4 grid-cols-1 sm:grid-cols-2 lg:grid-cols-3">
```

一行最多 3 张系统卡片；< 3 台时网格自然拉伸（`1fr`），宽度自适应。只渲染**有监控的系统**。

**系统卡片内**（监控块）：

- **最多 3 个**。`cols = min(3, monitors.length)`，按 `cols` 选网格类：

  ```ts
  function getMonitorGridClass(cols: number) {
    if (cols <= 1) return "grid-cols-1"
    if (cols === 2) return "grid-cols-1 @[340px]:grid-cols-2"
    return "grid-cols-1 @[340px]:grid-cols-2 @[560px]:grid-cols-3"
  }
  ```

- 用容器查询（`@container`，项目已在 `network-monitors-table.tsx:302` 使用）实现“窄卡片竖向堆叠、宽卡片并排 3 列”。
- **为什么竖向兜底而不是强行横向 3 列**：系统卡片在 `lg:grid-cols-3` 下内容宽约 350–420px，三等分后每块仅 110–140px，放不下「延迟 / 丢包率」两行指标 + 20 块色条。所以窄容器降级为 1 列（每块占满宽度，信息完整），宽容器才并排。
- **超过 3 个监控**：第 4 个起不渲染，卡片底部显示一行文字链接：

  ```tsx
  {monitors.length > 3 && (
    <button className="text-xs text-muted-foreground hover:text-foreground">
      +{monitors.length - 3} <Trans>more monitors</Trans>
    </button>
  )}
  ```

  点击 → 切到表格视图并预置该系统筛选。**不做水平滑动**：卡片内横向滚动在触控板上容易吞掉页面纵向滚动，体验差且实现不省事（需要 `snap` + 隐藏滚动条）。
- 排序：卡片内按 `loss1h` 降序（最差的排前面），保证 3 个名额留给最需要关注的监控。

#### 6.4.3 色块时间窗口与采样率（关键取舍）

**问题**：Agent 探测间隔由用户配置（默认 60s，可以是 300s），而色块数量固定 → 每块覆盖多长时间？采样率不匹配怎么办？

**决策：按“探测次数”聚合，不按“时间”聚合。** 这是全部问题的答案。

```
块规格（分级）：
  interval <= 120s  →  { blockMs: 2min,  count: 20,  type: "1m"  }   // 窗口 40 分钟
  interval >  120s  →  { blockMs: 10min, count: 20,  type: "10m" }   // 窗口 200 分钟
```

**为什么窗口是 40 分钟而不是 1 小时**：`network_monitor_stats` 的 `1m` 记录只保留 **1 小时**，且清理任务每 8 分钟跑一次（`records_deletion.go:79`，`{recordType:"1m", retention: time.Hour}`）。在 60 分钟边缘取数会随机拿到空数据。40 分钟窗口 + 20 块是安全值，也给实时查询留了余量。

**为什么长间隔要切到 `10m`**：若 interval=300s，40 分钟窗口内只有约 8 个样本 → 20 块里 12 块没数据，色条大面积发灰。切到 `10m` 记录（保留 12 小时）+ 每块 10 分钟，则每块恰好 1–2 个样本，色条依然饱满。

**聚合算法**（对 `network_monitor_stats` 的原始计数字段逐块求和）：

```ts
const blockMs = spec.blockMs
// 把 now 对齐到块边界，让整条色条每 blockMs 才平移一次，避免实时刷新时抖动
const nowMs = Math.floor(Date.now() / blockMs) * blockMs + blockMs

for (const r of records) {   // records: type = spec.type, created > nowMs - blockMs * 20
  const idx = Math.floor((nowMs - r.created) / blockMs)   // 0 = 最近一块（最右）
  if (idx < 0 || idx >= count) continue
  const b = blocks[idx]
  b.total   += r.total_count
  b.success += r.success_count
  b.sumUs   += r.res_sum
  if (r.success_count > 0) {
    b.minUs = Math.min(b.minUs, r.res_min)
    b.maxUs = Math.max(b.maxUs, r.res_max)
  }
}
```

**块结果与颜色判定（严格按此优先级）**：

```ts
function getBlockLevel(b, thresholds): MonitorLevel {
  if (b.total === 0) return "empty"                       // ① 该时段没有探测 → 灰，绝不是丢包
  if (b.success === 0) return "loss"                      // ② 全部失败 → 红（无延迟可算）
  const lossPct = (b.total - b.success) / b.total * 100
  if (lossPct >= 50) return "loss"                        // ③ 丢一半以上等同不可用 → 红
  return getMonitorLevel(b.sumUs / b.success, thresholds) // ④ 按成功样本平均延迟分绿/黄/红
}
```

**部分丢包（0 < lossPct < 50）的处理**：主色仍按延迟分档，但色块**底部加 1px 深红边**：

```tsx
className={cn(levelBg, hasPartialLoss && "border-b-2 border-red-700")}
```

这样一根色条同时表达两种信息：**整体色彩 = 延迟质量趋势，底部红线 = 该时段有丢包**。这正是 Komari 截图中「168ms / 15% 丢包 / 色条以黄绿为主带少量红」的表达方式 —— 如果按“有丢包就整块红”，15% 丢包的卡片会全红，反而看不出延迟趋势。

> ⚠️ **必须遵守的错误规避**：`total === 0` 表示“这段时间没探测过”（interval 长 / Agent 离线 / 刚加监控），**不能计入丢包**。Beszel 现有口径里丢包率是 `(total-success)/total`，分母为 0 时返回 0，前端必须显式区分 `empty` 与 `loss`。否则 5 分钟探测一次的监控会显示大面积“丢包”。

**采样率不匹配的影响（已接受）**：

| 场景 | 表现 |
|---|---|
| interval 60s，块 2min | 每块 2 个样本，精确 |
| interval 120s，块 2min | 每块 1 个样本，精确 |
| interval 300s，切到 10m / 块 10min | 每块 1–2 个样本，精确 |
| 刚加监控 / Agent 刚恢复 | 左侧块为 `empty`（灰），正常 |
| Hub 停机 30 分钟后恢复 | 中间若干块 `empty`（灰），正常 |

#### 6.4.4 卡片上的数字口径

| 指标 | 取值 | 理由 |
|---|---|---|
| 延迟 | `monitor.resAvg1h`（1 小时平均，微秒） | **用平均不用最后一次**：最后一次抖动大（单次 GC、单次重传就能让卡片闪一下），平均值才代表“这段时期的质量”。且与表格页一致，不会出现“卡片 42ms、表格 168ms”的困惑 |
| 丢包率 | `monitor.loss1h`（1 小时口径） | 与表格一致；已是百分比，直接 `toFixed(1)` |
| 兜底 | `resAvg1h === 0 && loss1h >= 100` → 显示 `丢包`（红） | 全丢时没有有效延迟，显示 0ms 是错的 |
| 无数据 | 两者为 0 且 `sampleCount === 0` → 显示 `—` | 刚添加的监控 |

**为什么不用色块窗口（40 分钟）算出的平均值做数字**：色条是给“趋势”看的，数字要稳定且与全站一致。两者时间范围略不同（40min vs 1h）不是问题 —— 数字给总体结论，色条给近期波动，互补。

鼠标悬停色块显示该块详情：`12:34–12:36 · 平均 168ms · 区间 120–340ms · 丢包 1/3 (33%) · 2 次探测`。

#### 6.4.5 视图切换

- 全局监控页 `routes/monitors.tsx` 顶部工具栏加 `LayoutList / LayoutGrid` 切换（照抄 `systems-table.tsx:274` 的 `DropdownMenuRadioGroup` 写法）。
- 持久化：`$userSettings.monitorViewMode`（`"table" | "grid"`），三级回退 `$userSettings` → `localStorage["besz-monitor-view"]` → 默认 `table`。**无需服务端迁移**（`user_settings.settings` 是 JSON blob）。
- 单系统详情页 `routes/system.tsx` 复用同一组件：`systemId` 存在时，卡片墙只渲染一张系统卡片，卡内仍最多 3 个监控块。

#### 6.4.6 数据加载（性能关键）

**不要**为每个监控各开一个 `useNetworkMonitorStats`（N 个监控 = N 次查询 + N 个 realtime 订阅）。

新增 Hook：`lib/use-monitor-cards.ts::useMonitorCardStats`

```ts
interface UseMonitorCardStatsProps {
  monitors: NetworkMonitorRecord[]      // 当前可见（已筛选）的监控
  thresholds: MonitorThresholds
}

interface MonitorCardData {
  blocks: QualityBlock[]                // 20 个，右对齐到 now
  sampleCount: number
}

// 返回 monitorId -> MonitorCardData
export function useMonitorCardStats(props): Map<string, MonitorCardData>
```

实现要点：

1. **按 `type` 分两组**（`1m` 组给 interval ≤120s 的监控，`10m` 组给其余），每组一次 `getFullList`：
   ```ts
   filter: (monitor={:m0} || monitor={:m1} || ...) && type={:type} && created>{:from}
   fields: "monitor,res_min,res_max,total_count,success_count,res_sum,created"
   ```
   最多 2 次请求。监控数 > 120 时按当前分页只取可见系统（卡片视图沿用 `DataTablePagination` 的分页状态）。
2. **一个** realtime 订阅（`network_monitor_stats` 的 `create`），**不带 `type` 过滤**，回调里按 `event.record.type` 分流到对应组的 buffer；200ms 批量 flush（沿用 `use-network-monitors.ts` 的 `pendingRaw` 模式）。
3. 分组聚合用纯函数 `buildQualityBlocks(records, spec, nowMs)`（放 `lib/monitor-blocks.ts`，**可单测，不依赖 React**）。
4. 只在数据真变了才更新 map（比较最后一条 `created`），避免每分钟整墙重渲染。

**色块渲染用纯 CSS `div`**（不需要 SVG、不需要 Recharts）：20 个 `<div>` + `flex-1` + 背景色类，成本接近零。

#### 6.4.7 空态

- 没有任何监控 → 保持现状的 “No results.”
- 某系统刚添加监控还没数据 → 该监控块显示 `—` + 20 个 `empty`（灰）块，不显示骨架屏（避免闪烁）。

### 6.5 3.5 TCP / UDP 连接数（可选，后置）

**现状核查结论**：Agent **完全没有采集连接数**。`system.Stats`（`entities/system/system.go:23-62`）里没有任何 connections 字段，`agent/network.go` 只用 `psutilNet.IOCounters(true)` 取流量计数器。

**若要实现**（建议放 P10，非主线）：

- 数据：`system.Stats` 加 `Connections [2]uint16`（`json:"conn,omitzero"`，`cbor:"43,keyasint,omitzero"`），语义 `[tcp, udp]`。cbor key 43 是下一个空闲编号（42 已被 `SwapBytes` 占用）。
- 采集（仅 Linux，零外部依赖）：读 `/proc/net/tcp`、`/proc/net/tcp6` 统计 `st == "01"`（ESTABLISHED）的行数；读 `/proc/net/udp`、`/proc/net/udp6` 统计全部行数（UDP 无连接状态）。
- 非 Linux：返回 0（`omitzero` 不发送），前端不显示。
- 展示位置：**系统卡片**（机器级指标，不是监控级）。空间紧张时折叠为 `2.1k conns` 小字；或只放在系统详情页的「网络」图表区顶部。
- 成本评估：单次 read + 字符串解析，几千个 socket 时 < 5ms，每 60s 一次，可忽略。

**不推荐**用 `gopsutil net.Connections("tcp")`：它需要遍历 `/proc/*/fd` 反查 inode → 进程，在 1000+ 连接时开销显著，也没必要引入进程维度。

**结论**：列入 P10.4，若时间紧可整体跳过，不影响其他功能。


## 7. 功能四：系统卡片上下行分开显示

### 7.1 数据

`internal/entities/system/system.go::Info` 追加：

```go
BandwidthUp   uint64 `json:"bu,omitempty" cbor:"28,keyasint,omitempty"` // bytes/s sent
BandwidthDown uint64 `json:"bd,omitempty" cbor:"29,keyasint,omitempty"` // bytes/s recv
```

`agent/system.go:272` 附近：

```go
a.systemInfo.BandwidthBytes = systemStats.Bandwidth[0] + systemStats.Bandwidth[1]
a.systemInfo.BandwidthUp   = systemStats.Bandwidth[0]
a.systemInfo.BandwidthDown = systemStats.Bandwidth[1]
```

### 7.2 兼容

- **旧 Agent** 不发送 `bu/bd` → 两者为 0 → 前端回退：`bu/bd` 均为 0 且 `bb > 0` 时显示合并值（保持旧观感，加 `↑↓` 合并图标）。
- **Hub 无需版本门槛**，因为字段是可选的，零值即“未知”。
- `Info` 会自动存入 `systems.info`（`system.go:342`），前端 `SystemInfo` 类型加 `bu?: number; bd?: number`。

### 7.3 展示

系统卡片网络列/单元格：

```
↑ 1.2 MB/s
↓ 8.4 MB/s
```

表格视图单行显示 `↑1.2 M · ↓8.4 M`（紧凑格式）。移动端（卡片窄）上下两行，桌面端一行。单位格式化复用现有 `formatBytes` 系列工具。

---

## 8. 权限与多用户

| 集合 | list/view | create/update/delete |
|---|---|---|
| `system_billing` | `systemScopedReadRule` | `systemScopedWriteRule`（readonly 用户被挡） |
| `system_traffic` | `systemScopedReadRule` | **全部 nil**（Hub 独占） |

- `SHARE_ALL_SYSTEMS=true` 时读放宽为“任意登录用户”，写仍受 `role != "readonly"` 限制（沿用现有 `collections.go` 变量）。
- 到期/流量通知：发给 `systems.users` 中每个用户；`SendAlert` 内部再校验一次权限与免打扰。
- readonly 用户在 `BillingDialog` 里看到只读视图（`disabled`），不显示「立即重置」。

---

## 9. 迁移与兼容性

### 9.1 迁移清单

| 迁移 | 内容 |
|---|---|
| `1xxx_network_monitor_name.go` | `network_monitors` + `name` Text(100) |
| `1xxx_system_billing.go` | 新建 `system_billing` 集合（含全部费用+流量字段 + 唯一索引） |
| `1xxx_system_traffic.go` | 新建 `system_traffic` 集合（含唯一索引） |

迁移写法参考 `1790273174_network_monitor_server.go`（增量加字段）与 `0_collections_snapshot_0_20_0.go`（整集合 JSON），并在 `internal/hub/collections.go::setCollectionAuthSettings` 注册权限（**必须在 `StartHub` 里被调用到**）。

### 9.2 兼容性

| 维度 | 影响 |
|---|---|
| 旧 Agent + 新 Hub | 全部正常工作；`bu/bd` 为 0（回退合并显示）；900ms 计丢包不生效（旧 Agent 未归一化） |
| 新 Agent + 旧 Hub | 新字段被忽略（CBOR 未知 key 忽略）；无破坏 |
| 已存在的 `network_monitors` | `name` 为空 → 显示行为完全不变 |
| 现有测试 | `network_monitor_history_test.go` 用 `responseUs: 1000` 等小值，不受 900ms 阈值影响；需新增“慢样本计丢包”用例 |
| 前端缓存 | `localStorage["besz-monitor-cols"]` 等不受影响；新增 `monitorViewMode` 走 `user_settings` |

### 9.3 版本号

- 建议把 fork 版本推到 `0.22.0-beszelx.1`（`beszel.go::Version`），并新增 `MinVersionBandwidthDirections = 0.22.0`（虽然不强依赖，但便于前端判断是否显示 ↑↓ 而不是回退）。
- `beszel.MinVersionNetworkMonitors` 不变。

---

## 10. 非功能要求

| 项 | 要求 |
|---|---|
| 性能 | 卡片墙 50 个系统 / 200 个监控：首屏 ≤ 2 个查询 + 1 个订阅；滚动 60fps；色块渲染 < 16ms/帧 |
| 写入放大 | 新增写：每系统每 60s 1 次 `system_traffic` 更新（约 200 字节），相对 `system_stats` 可忽略 |
| 存储 | 无新增时序数据（流量只存运行态）；成本/流量配置每系统 1 行 |
| i18n | 所有新文案走 Lingui（`t\`\`` + `Trans`），更新 `i18n.yml` 与 `locales/*` |
| 可访问性 | 进度条 `role="progressbar"` + `aria-valuenow`；颜色不作为唯一信息载体（同时显示数值） |
| 深色模式 | 全部颜色使用语义类（`text-emerald-600 dark:text-emerald-400`），不硬编码十六进制 |
| RTL | 布局用逻辑属性（`ms-*`/`me-*`），色条在 RTL 下反向（右边仍为最新） |
| 日志 | 流量重置、异常差值、提醒发送均 `Logger().Info/Warn` 记录，便于排障 |

---

## 11. 验收标准

### 成本

- [ ] 卡片横点菜单出现「计费与流量」，能保存 `付费/免费`、4 种货币、6 种周期。
- [ ] 填 `paidAt` + `cycle` 点「按周期推算」能正确填出 `expiresAt`（含月末边界）。
- [ ] 卡片显示 `¥25/月 · 剩余 12 天`，<7 天变红，已过期显示 `已过期 N 天`。
- [ ] `free` 显示「免费」，`permanent` 显示「永久」且无到期日。
- [ ] 手动把 `expiresAt` 改到 6 天后，次日 Cron 发出一次邮件/Webhook；第三天不再重复发；第 3/1/0 天各发一次。
- [ ] 续费（把 `expiresAt` 往后推）后 `remindLevel` 归零，能再次提醒。
- [ ] readonly 用户看不到保存按钮/输入框禁用。

### 流量

- [ ] 设置 `1 TB / 每月 14 日 / 上下行合计`，卡片出现进度条与「N 天后重置」。
- [ ] 造 500MB 上下行流量 → `usedUpload/usedDownload` 与真实值误差 < 1%。
- [ ] 把 Hub 停 2 小时后重启 → 已用量一次性补齐，误差 < 1%。
- [ ] 重启被监控机器 → 已用量**不回退**、不出现负数；差值触发归零补偿。
- [ ] 拨到 `仅计上行` → 卡片「已用」只按上行算，拆分小字仍显示两向。
- [ ] 跨过重置日 → 已用量归零、`periodStart/periodEnd` 顺延、`trafficRemindLevel` 归零。
- [ ] `resetDay=31` 在 2 月按 28/29 号重置。
- [ ] 手动「立即重置」→ ≤60s 内归零。
- [ ] `trafficLimit=0` → 卡片不显示流量区。

### 900ms 计丢包

- [ ] 用一个故意延迟 1200ms 的 TCP 目标：`loss1h` ≈ 100%，`res_avg` 为空/不显示，图表出现 100% 丢包。
- [ ] 延迟 500ms 的目标：显示红色延迟，**不计丢包**。
- [ ] 延迟 100ms：绿色。
- [ ] `MONITOR_LOSS_THRESHOLD_MS=0` 关闭归一化后行为回到 v0.21.1。
- [ ] 丢包告警在慢目标上按阈值正常触发。

### 监控名称

- [ ] 添加监控时可填名称，编辑可改；列表/卡片/Sheet 标题/图表图例/搜索都优先显示名称。
- [ ] 清空名称后退回显示 target，行为与 v0.21.1 一致。
- [ ] 修改名称**不导致**监控 ID 变化（`network_monitor_stats` 历史连续，不出现断点）。

### 卡片视图

- [ ] 全局监控页可切 `表格 / 卡片`，刷新后保持。
- [ ] 卡片按系统聚合，一行最多 3 张；2 台系统时各占一半宽。
- [ ] 卡内最多显示 3 个监控块，第 4 个起折叠为 `+N more`，点击跳到表格视图并带上该系统筛选。
- [ ] 卡内监控数 ≤3 时自适应（1 个占满、2 个各半、3 个三等分）；窄容器（<340px）自动竖向堆叠。
- [ ] 每个监控块显示：名称 + 协议标签 / 平均延迟（`resAvg1h`）/ 丢包率（`loss1h`）/ 20 个色块。
- [ ] 色块颜色随 150/400ms 变化；**全丢或丢包 ≥50% 显示红块**；**部分丢包在色块底部显示红边**；**无探测样本显示灰块**。
- [ ] 刚添加、尚无数据的监控：延迟与丢包显示 `—`，20 个全灰块（不显示 100% 丢包）。
- [ ] 把某监控 interval 设为 300s，色条仍饱满（自动切到 10m 记录 / 每块 10min）。
- [ ] 点击监控块打开原有 Sheet，图表正常（折线等精确图表只在这里）。
- [ ] 200 个监控时页面滚动不卡（CPU 无明显飙升）。
- [ ] 色块 hover 显示块详情（时间段 / 平均 / 最小最大 / 丢包次数）。

### 上下行

- [ ] 新 Agent：卡片同时显示 `↑` `↓` 两个值。
- [ ] 旧 Agent：回退显示合并值，不出现 `↑0 B/s ↓0 B/s`。

---

## 12. 开放问题与默认决策

| # | 问题 | 默认决策（已写入方案，若不同意需在开工前修改） |
|---|---|---|
| Q1 | 成本配置放 `systems` 字段还是独立集合？ | **独立 `system_billing`**（理由见 §0 第 3 条） |
| Q2 | 到期日手动填还是自动推算？ | **手动填为真相**，提供推算按钮 |
| Q3 | 免打扰时段内到期提醒是否仍要送达？ | **遵守免打扰**（保持一致）；若需必达，后续给 `SendAlert` 加 `IgnoreQuietHours` |
| Q4 | 提醒/重置的时区？ | **Hub 进程本地时区**，部署时设 `TZ` |
| Q5 | 提醒发给谁？ | **系统所有成员**（`systems.users`） |
| Q6 | 150/400ms 阈值是全局还是按监控？ | **全局用户设置**（`user_settings.monitorWarnMs` / `monitorCritMs`，设置页 → General 可改）。**已确认：不做 per-monitor 覆盖，后续也不需要** |
| Q7 | 900ms 是否适用所有协议？ | **是**（HTTP/TCP/ICMP/DNS 统一），通过 Agent 环境变量可调 |
| Q8 | 流量是否要历史曲线？ | **MVP 不做**；若要做，用现有 10 分钟聚合 Cron 增量落 `traffic_daily`，作为独立阶段 |
| Q9 | 暂停系统是否计流量？ | **不计**，卡片灰显提示 |
| Q10 | 是否要"不限流量"？ | **要**（`trafficLimit=0` 表示关闭） |
| Q11 | 流量阈值提醒 80%/100% 是否要做？ | **做，但排到最后一个阶段**（P10.1） |
| Q12 | 卡片色块的时间窗口？ | **20 块 × 2 分钟 = 40 分钟**（interval >120s 时切 20 块 × 10 分钟 = 200 分钟，用 `10m` 记录）。理由见 §6.4.3 |
| Q13 | 卡片上的延迟数字用哪个口径？ | **`resAvg1h`（1 小时平均）**，不用最后一次探测，也不用色块窗口均值。理由见 §6.4.4 |
| Q14 | 丢包在色块上怎么表达？ | **全丢/≥50% 丢包 = 整块红；0–50% 部分丢包 = 延迟主色 + 底部红边**。不做第 4 种独立颜色（会破坏绿黄红的直觉） |
| Q15 | 卡片内超过 3 个监控怎么办？ | **折叠为 `+N more` 链接**，不做水平滑动（触控板会吞页面纵向滚动） |
| Q16 | 是否做 TCP/UDP 连接数？ | **Agent 目前未采集**。作为 P10.4 可选项：仅 Linux 读 `/proc/net/{tcp,udp}{,6}`，非 Linux 隐藏 |

---

## 13. 与 Komari 的差异说明（供参考，不影响实现）

| 能力 | Komari | 本方案 |
|---|---|---|
| 价格/周期 | 有 | 有，且支持「永久」「免费」 |
| 到期提醒 | 有（阈值可配） | 有（多档 7/3/1/0 + 去重） |
| 流量统计 | Agent 侧累计 | **Hub 侧差分累计**（抗重启更强，Agent 零改动） |
| 延迟卡片 | 有（单监控一张卡 + 竖状色块） | 有（按系统聚合，卡内最多 3 个监控块，同样用竖状色块） |
| 绿黄红 | 固定阈值 | 用户可配（默认 150/400ms，全局） |
| 丢包表达 | 数字 + 色块内红色 | 数字 + 全丢红块 / 部分丢包底部红边 |
| 连接数 | 有 | 默认不做（P10.4 可选，仅 Linux） |
| 慢即丢包 | 有 | 有（900ms，Agent 侧统一口径） |
