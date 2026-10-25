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
- [ ] P7 — 通知文案是英文硬编码（与 `alerts_*.go` 一致），未走 Lingui。→ 若要中文化，改 `reminders.go::expiryMessage` 一个函数即可。
- [ ] P7 — 到期提醒没做人工验收（改 `expiresAt` 为明天 + 把 Cron 临时改成 `* * * * *` 观察）。→ 需要真实 hub 实例；自动化覆盖见 `billing_reminders_test.go`。
- [ ] P8（预判）— `AccumulateTraffic` 在事务内做 2 次额外查询（每系统每分钟）。个人规模无感，系统数 >100 时考虑加内存缓存。
