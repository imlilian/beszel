# 遗留问题与待办

> 记录「发现但不在当前阶段修」的问题。每阶段开始时先扫一眼，避免重复踩坑。
> 格式：`- [ ] 阶段标记 — 问题 — 建议处理阶段`

## 环境 / 工具链

- [ ] P0 — Windows 上 `go test ./agent/` 有 2 个固有失败（`TestLegacyHubGetsSinglePayload` 依赖 Unix socket、`TestBuildFsStatRegistration`）。非本次改动引入。→ 不修，作为基线；改 agent 代码时用 `GOOS=linux go build ./...` 兜底。
- [ ] P0 — 干净检出直接 `go test ./...` 会因三处 `go:embed` 目录缺失而 `[setup failed]`。已写进 `docs/BASELINE.md` §2。→ 可选：后续给 Makefile 加一个 `prepare-embeds` 目标。
- [ ] P0 — `npm install` 会改写 `internal/site/package-lock.json`（本机 npm 版本差异）。→ 每次提交前 `git checkout -- internal/site/package-lock.json`。

## 网络监控

- [ ] P1 — 批量添加/复制监控的 CSV 格式新增了可选的 `name` 列（第 6 位），旧 5 列输入仍兼容。若用户名称里含逗号会解析错位。→ 视需要改为带引号的 CSV 解析（低优先级）。
- [ ] P1 — `system/target` 相关排序仍按显示名排序；监控数量很多时同一 target 在多个系统上的排序行为未变。→ 保持现状。

## 计费 / 流量

- [ ] P5（预判）— `system_traffic` 每 60s 更新一次，若同时订阅全量记录，500 台系统时前端会有明显重渲染压力。→ 若发生，改为只订阅可见分页的系统。
- [x] P7 — 到期提醒的开关接入现有 `alerts` 系统：新增 `BillingExpiry` 告警类型（迁移 `1793010002` 追加 `alerts.name` 枚举 + `lib/alerts.ts::alertInfo` 纯开关 + Cron 只给该 user+system 存在记录的发送）。**不要**再给 `system_billing` 加 `expiryAlerts` bool。阈值仍用 `system_billing.notifyDaysBefore`。已完成。
- [ ] P7 — 「警报」面板里 `BillingExpiry` 对**免费 / 永久**系统也会显示（面板按 `alertInfo` 全量渲染），打开后不会有任何通知。→ 可选：在 `AlertDialogContent` 里按 `$billingBySystemId` 过滤掉 `billingType !== "paid"` 的系统，或给它加一条 `note`。
- [ ] P7 — 去重状态 `remindLevel` 存在 `system_billing`（全局），多用户共享一个系统时"某用户后开开关"会错过已推进的档位。→ 需要严格按用户去重时，把已提醒档位写进该 `alerts` 记录的 `state` JSON（参考 `NetworkMonitorCert`）。
- [x] P7 — 通知正文的语言：**确认与上游架构一致，不是缺陷，无需处理**。Beszel 的邮件/Webhook 正文一律由 Go 在投递前拼成字符串（`alerts_system.go` / `alerts_container.go` / `alerts_network_monitors.go` / `alerts_zfs.go` / `alerts_smart.go` / `alerts_systemd.go` 全是英文 `fmt.Sprintf`），经 `AlertManager.SendAlert` 交给 shoutrrr/SMTP；触发方是 Cron 或心跳入库，**整条链路没有前端参与**，Lingui 目录覆盖不到。前端负责的是展示层（`alertInfo` 的名称/单位/描述、告警历史表），那部分已经是多语言。`reminders.go::expiryMessage` 沿用同一模式。→ 若将来确实要发中文邮件，需在 Go 侧加按 `user_settings.settings.lang` 分发的消息表，并补上「切换语言时回写 `settings.lang`」（目前全仓没有任何代码写该字段，`api.ts:94` 只读不写）。
- [ ] P7 — 到期提醒没做人工验收（改 `expiresAt` 为明天 + 把 Cron 临时改成 `* * * * *` 观察）。→ 需要真实 hub 实例；自动化覆盖见 `billing_reminders_test.go`。
- [ ] P8 — 采样事务里多了 2 次查询（`system_billing` + `system_traffic`）和 1 次写，每系统每分钟一次。个人规模无感；系统数 >100 时考虑把 billing 配置放进内存缓存。
- [ ] P8 — 只统计 `trafficLimit > 0` 的系统：先不限额、后设限额时，历史用量从设置那一刻起算（前端本来就只在有上限时渲染 `TrafficBar`，语义一致）。→ 若将来要"无限额也留历史"，把 `AccumulateTraffic` 的 `TrafficLimit <= 0` 提前返回去掉即可，代价是每个系统每分钟多一次写。
- [ ] P8 — 依赖 Agent 上报 `Stats.NetworkInterfaces`（累计计数器）。0.13.0 之前的老 Agent 没有该字段，永远不累计；升级 Agent 后首个采样点只建基线。
- [ ] P9 — 流量 Tab 的「立即重置」在**没有 `system_traffic` 记录**时整块面板不渲染（因此没有按钮）。→ 若要允许"提前建记录"，让 `AccumulateTraffic` 在 `trafficLimit > 0` 时即使无网卡也建一条空记录即可（代价：老 Agent 会有一条全 0 的记录）。
- [ ] P9 — `TrafficUsage` 的剩余时间是整天粒度（`getTrafficState.daysLeft`），PRD §5.5 草图写的是「18 天 6 小时」。→ 需要精确到小时时，改成直接对 `periodEnd - now` 做 `ms → d/h` 拆分（注意跨夏令时）。
- [ ] P9 — 未做人工验收（造 500MB 流量、停 Hub 2 小时、重启被监控机、跨重置日、手动重置）。→ 需要真实 hub + agent；纯函数与后端逻辑已被 `bun test` / `go test` 覆盖。
- [ ] P10.1 — 「警报」面板里的 `TrafficQuota` 对**没设流量上限**的系统也会显示（面板按 `alertInfo` 全量渲染），打开后不会有任何通知。→ 与 `BillingExpiry` 同一个待办，可一并按 `$billingBySystemId` 过滤。
- [ ] P10.1 — `system_billing.trafficAlerts` 列已成为死字段（前端不读不写、Go 只解码）。→ 想清理时写一个迁移删列，注意回滚会丢值；也可以永久留着，成本为零。
- [ ] P10.1 — 流量提醒跑每小时，最坏情况下「刚过 80%」到「收到通知」相差不到 1 小时。→ 若要求更快，把 Cron 改成 `*/15 * * * *` 即可（档位去重保证不会重复发）。
- [ ] P10.1 — 未做人工验收（把某个系统用量改到 81% / 101% 观察邮件）。→ 需要真实 hub + 通知渠道；自动化覆盖见 `traffic_reminders_test.go`。
- [x] P10.4 — TCP/UDP 连接数：**用户决定不做**（2026-10-25）。PRD §6.5 与 Q16 已标注，WORKPLAN 保留调研记录。
- [ ] 版本 — **自更新按钮会覆盖 fork**：`internal/ghupdate` 拉的是上游 `henrygd/beszel` 的 release 资产，点「更新」会把 fork 二进制换成上游版本。当前版本号 `0.21.1+beszelx.1` 只保证上游 0.21.x 不被提示，上游一发布 0.21.2 就会提示。→ 若要彻底避免：把 `ghupdate.Config` 的仓库指到自己的 fork，或隐藏前端更新入口。
- [ ] 版本 — 合并上游后忘记改版本号前半段。→ 合并 checklist 第一步就是 `beszel.go::Version` 的 `+` 前部分对齐新的上游基线；`beszel_test.go` 只能保证格式，不能保证对齐。
