/**
 * Pure billing helpers shared by the dialog, card summary, and unit tests.
 *
 * These functions are intentionally free of any React / i18n / PocketBase
 * imports so they can be unit-tested with `bun test` without a DOM or a
 * compiled catalog. Display strings that need translation are composed in the
 * components (see billing-dialog.tsx), which call into these pure helpers.
 */

export type Currency = "CNY" | "USD" | "EUR" | "GBP"
export type Cycle = "monthly" | "semiannual" | "annual" | "biennial" | "triennial" | "permanent"
export type TrafficMode = "sum" | "upload"

/** Currency glyphs are locale-independent, so they live here. */
export const CURRENCY_SYMBOL: Record<Currency, string> = {
	CNY: "¥",
	USD: "$",
	EUR: "€",
	GBP: "£",
}

/** Neutral short cycle label (e.g. `/mo`). The localized version is built in the component. */
export function formatCycleShort(cycle: Cycle): string {
	switch (cycle) {
		case "monthly":
			return "/mo"
		case "semiannual":
			return "/6mo"
		case "annual":
			return "/yr"
		case "biennial":
			return "/2yr"
		case "triennial":
			return "/3yr"
		case "permanent":
			return ""
	}
}

/** `25 -> "¥25"`, `25.5 -> "¥25.50"`. Integers drop decimals; non-integers keep two. */
export function formatPrice(price: number, currency: Currency): string {
	const symbol = CURRENCY_SYMBOL[currency] ?? ""
	const safe = Number.isFinite(price) ? price : 0
	const num = Number.isInteger(safe) ? safe.toFixed(0) : safe.toFixed(2)
	return `${symbol}${num}`
}

/**
 * Whole calendar days from "today 00:00" to "target 00:00" (UTC day boundaries),
 * mirroring the Go `DaysUntilExpiry` arithmetic so a DST change can never shave
 * a day off. Negative means the target is in the past. A non-positive target
 * returns 0 (callers must check the source value first).
 */
export function daysUntilExpiry(targetMs: number, now: number = Date.now()): number {
	if (targetMs <= 0) return 0
	const n = new Date(now)
	const t = new Date(targetMs)
	const today = Date.UTC(n.getFullYear(), n.getMonth(), n.getDate())
	const targetDay = Date.UTC(t.getUTCFullYear(), t.getUTCMonth(), t.getUTCDate())
	return Math.round((targetDay - today) / 86_400_000)
}

export type ExpiryState = {
	kind: "none" | "active" | "expired"
	daysLeft?: number
}

/**
 * Classify an expiry timestamp. `free` / `permanent` are decided by the caller
 * (they depend on billingType / cycle, which this function does not receive);
 * a zero timestamp means "no expiry tracked" (`kind: "none"`, no `daysLeft`).
 */
export function getExpiryState(expiresAtMs: number, now: number = Date.now()): ExpiryState {
	if (!expiresAtMs || expiresAtMs <= 0) return { kind: "none" }
	const daysLeft = daysUntilExpiry(expiresAtMs, now)
	if (daysLeft <= 0) return { kind: "expired", daysLeft }
	return { kind: "active", daysLeft }
}

/** Expiry urgency band used for coloring. `>=30` ok, `7..29` warn, `<7` crit. */
export function getExpirySeverity(daysLeft: number): "ok" | "warn" | "crit" {
	if (daysLeft >= 30) return "ok"
	if (daysLeft >= 7) return "warn"
	return "crit"
}

export interface TrafficState {
	/** 0..100 percentage of the quota consumed. 0 when unlimited. */
	pct: number
	/** Bytes remaining (never negative). */
	remaining: number
	/** Whole days until the period resets (0 when unlimited). */
	daysLeft: number
	/** True when usage has passed the quota (unlimited never exceeds). */
	exceeded: boolean
}

/**
 * Derived traffic view for a quota + used bytes + reset timestamp.
 * `used` is the already-mode-adjusted byte count (sum or upload-only).
 */
export function getTrafficState(limit: number, used: number, periodEndMs: number, now: number = Date.now()): TrafficState {
	const safeLimit = limit > 0 ? limit : 0
	const pct = safeLimit > 0 ? Math.min(100, (used / safeLimit) * 100) : 0
	const remaining = Math.max(0, safeLimit - used)
	const daysLeft = safeLimit > 0 ? daysUntilExpiry(periodEndMs, now) : 0
	const exceeded = safeLimit > 0 && used > safeLimit
	return { pct, remaining, daysLeft, exceeded }
}

/* ------------------------------------------------------------------ */
/* Date helpers (PB stores dates as "2026-03-14 00:00:00.000Z")        */
/* ------------------------------------------------------------------ */

/** Parse a PocketBase date string into epoch ms (0 when empty / invalid). */
export function parsePbDate(value: string): number {
	if (!value) return 0
	const ms = Date.parse(value.replace(" ", "T"))
	return Number.isNaN(ms) ? 0 : ms
}

/** PB date string -> `YYYY-MM-DD` for a native `<input type="date">` ("" when empty). */
export function toDateInputValue(value: string): string {
	const ms = parsePbDate(value)
	if (!ms) return ""
	return new Date(ms).toISOString().slice(0, 10)
}

/** `YYYY-MM-DD` -> a Date at UTC midnight, or `null` when empty. */
export function fromDateInputValue(value: string): Date | null {
	if (!value) return null
	const ms = Date.parse(`${value}T00:00:00.000Z`)
	return Number.isNaN(ms) ? null : new Date(ms)
}

/* ------------------------------------------------------------------ */
/* addCycle — must match Go billing.AddCycle (UTC, month-end clamping)  */
/* ------------------------------------------------------------------ */

const CYCLE_MONTHS: Record<Cycle, number> = {
	monthly: 1,
	semiannual: 6,
	annual: 12,
	biennial: 24,
	triennial: 36,
	permanent: 0,
}

function daysInMonthUTC(year: number, month0: number): number {
	// day 0 of the *next* month is the last day of `month0` (0-based)
	return new Date(Date.UTC(year, month0 + 1, 0)).getUTCDate()
}

/**
 * Returns the date one billing cycle after `date`, keeping the time of day.
 * Day-of-month is clamped to the target month's last day
 * (Jan 31 + 1 month = Feb 28/29), matching Go's `AddCycle`. `permanent`
 * (and any unknown cycle) returns the zero time, i.e. no expiry to compute.
 */
export function addCycle(date: Date, cycle: Cycle): Date {
	const months = CYCLE_MONTHS[cycle]
	if (!months) return new Date(0)
	const d = new Date(date)
	const total = d.getUTCFullYear() * 12 + d.getUTCMonth() + months
	const year = Math.floor(total / 12)
	const month = total % 12 // 0-based
	let day = d.getUTCDate()
	const last = daysInMonthUTC(year, month)
	if (day > last) day = last
	return new Date(
		Date.UTC(year, month, day, d.getUTCHours(), d.getUTCMinutes(), d.getUTCSeconds(), d.getUTCMilliseconds())
	)
}
