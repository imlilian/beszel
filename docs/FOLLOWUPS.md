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
- [ ] P6 — 到期提醒**没有按系统的开关**：`system_billing` 只有 `notifyDaysBefore`（提前天数），投递开关是用户通知设置（email/webhook）里的全局配置；`notifyDaysBefore=0` 在 `ReminderLevel` 中等于「不提醒」，但 create hook 会把 `<=0` 重置为 7，所以 0 存不下来。若要「某系统单独关闭到期提醒」，需新增 `expiryAlerts` bool（或允许显式 0）。→ 建议 P7（cron）一起加，P7 的 `ReminderLevel` 已天然支持 0 档跳过。
- [ ] P8（预判）— `AccumulateTraffic` 在事务内做 2 次额外查询（每系统每分钟）。个人规模无感，系统数 >100 时考虑加内存缓存。
