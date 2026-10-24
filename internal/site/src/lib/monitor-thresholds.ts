import type { UserSettings } from "@/types"

/**
 * Display-only latency thresholds, in milliseconds.
 *
 * These decide colors and nothing else. The "slow responses count as loss"
 * boundary lives on the agent (MONITOR_LOSS_THRESHOLD_MS, default 900ms) because
 * it changes what the data means. Keeping the two separate is deliberate: one is
 * a shared measurement rule, the other is personal taste.
 */
export interface MonitorThresholds {
	warnMs: number
	critMs: number
}

export const DEFAULT_MONITOR_THRESHOLDS: MonitorThresholds = { warnMs: 150, critMs: 400 }

export type MonitorLevel = "good" | "warn" | "crit" | "loss" | "empty"

/** Settings are stored as submitted form values (strings), so coerce and clamp. */
function toPositiveInt(value: unknown, fallback: number): number {
	const n = typeof value === "number" ? value : Number.parseInt(String(value ?? ""), 10)
	return Number.isFinite(n) && n > 0 ? Math.round(n) : fallback
}

export function getMonitorThresholds(settings?: Partial<UserSettings> | null): MonitorThresholds {
	const warnMs = toPositiveInt(settings?.monitorWarnMs, DEFAULT_MONITOR_THRESHOLDS.warnMs)
	const rawCritMs = toPositiveInt(settings?.monitorCritMs, DEFAULT_MONITOR_THRESHOLDS.critMs)
	// A critical threshold at or below the warning one would make the yellow band unreachable.
	return { warnMs, critMs: Math.max(rawCritMs, warnMs + 1) }
}

/**
 * Classify a response time. Missing or negative values are loss: the agent
 * reports -1 for failed probes and for probes slower than its loss threshold.
 */
export function getMonitorLevel(
	resUs: number | null | undefined,
	thresholds: MonitorThresholds = DEFAULT_MONITOR_THRESHOLDS
): MonitorLevel {
	if (resUs == null || resUs < 0) return "loss"
	if (resUs <= thresholds.warnMs * 1000) return "good"
	if (resUs <= thresholds.critMs * 1000) return "warn"
	return "crit"
}

/** Dot / block background colors, matching the meter colors used elsewhere. */
export const monitorLevelBg: Record<MonitorLevel, string> = {
	good: "bg-green-500",
	warn: "bg-yellow-500",
	crit: "bg-red-500",
	loss: "bg-red-600",
	empty: "bg-muted-foreground/30",
}

/** Text colors for latency values. */
export const monitorLevelText: Record<MonitorLevel, string> = {
	good: "",
	warn: "text-yellow-600 dark:text-yellow-500",
	crit: "text-red-500",
	loss: "text-red-500",
	empty: "text-muted-foreground",
}
