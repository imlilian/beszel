import { describe, expect, test } from "bun:test"
import {
	addCycle,
	daysUntilExpiry,
	formatPrice,
	getBillingExpiryState,
	getExpirySeverity,
	getExpiryState,
	getTrafficSeverity,
	getTrafficState,
	pickTrafficUnit,
	trafficFromBytes,
	trafficToBytes,
} from "./billing-utils"

describe("formatPrice", () => {
	test("integer drops decimals", () => {
		expect(formatPrice(25, "CNY")).toBe("¥25")
	})
	test("non-integer keeps two decimals", () => {
		expect(formatPrice(25.5, "CNY")).toBe("¥25.50")
	})
	test("zero integer", () => {
		expect(formatPrice(0, "CNY")).toBe("¥0")
	})
	test("other currencies", () => {
		expect(formatPrice(10, "USD")).toBe("$10")
		expect(formatPrice(1.25, "GBP")).toBe("£1.25")
		expect(formatPrice(0, "EUR")).toBe("€0")
	})
	test("NaN/Infinity falls back to 0", () => {
		expect(formatPrice(Number.NaN, "CNY")).toBe("¥0")
	})
})

describe("daysUntilExpiry", () => {
	const now = Date.UTC(2026, 2, 1) // Mar 1 2026
	test("future positive day count", () => {
		expect(daysUntilExpiry(Date.UTC(2026, 2, 13), now)).toBe(12)
	})
	test("past negative day count", () => {
		expect(daysUntilExpiry(Date.UTC(2026, 1, 20), now)).toBe(-9)
	})
	test("zero target returns 0", () => {
		expect(daysUntilExpiry(0, now)).toBe(0)
	})
})

describe("getExpiryState", () => {
	const now = Date.UTC(2026, 2, 1)
	test("future -> active", () => {
		const s = getExpiryState(Date.UTC(2026, 2, 13), now)
		expect(s.kind).toBe("active")
		expect(s.daysLeft).toBe(12)
	})
	test("expiry day itself -> active with 0 days left", () => {
		const s = getExpiryState(Date.UTC(2026, 2, 1), now)
		expect(s.kind).toBe("active")
		expect(s.daysLeft).toBe(0)
	})
	test("past -> expired", () => {
		const s = getExpiryState(Date.UTC(2026, 1, 20), now)
		expect(s.kind).toBe("expired")
		expect(s.daysLeft).toBeLessThan(0)
	})
	test("zero -> none", () => {
		expect(getExpiryState(0, now).kind).toBe("none")
	})
})

describe("getBillingExpiryState", () => {
	const now = Date.UTC(2026, 2, 1)
	test("free wins over every other field", () => {
		expect(getBillingExpiryState("free", "monthly", Date.UTC(2026, 2, 13), now).kind).toBe("free")
	})
	test("permanent has no expiry", () => {
		expect(getBillingExpiryState("paid", "permanent", Date.UTC(2026, 2, 13), now).kind).toBe("permanent")
	})
	test("paid without an expiry -> none", () => {
		expect(getBillingExpiryState("paid", "monthly", 0, now).kind).toBe("none")
	})
	test("active carries daysLeft", () => {
		expect(getBillingExpiryState("paid", "monthly", Date.UTC(2026, 2, 13), now)).toEqual({
			kind: "active",
			daysLeft: 12,
		})
	})
	test("expired carries negative daysLeft", () => {
		const s = getBillingExpiryState("paid", "monthly", Date.UTC(2026, 1, 20), now)
		if (s.kind !== "expired") throw new Error(`expected expired, got ${s.kind}`)
		expect(s.daysLeft).toBeLessThan(0)
	})
})

describe("getExpirySeverity", () => {
	test(">=30 ok", () => expect(getExpirySeverity(30)).toBe("ok"))
	test("7..29 warn", () => expect(getExpirySeverity(7)).toBe("warn"))
	test("<7 crit", () => expect(getExpirySeverity(6)).toBe("crit"))
	test("negative crit", () => expect(getExpirySeverity(-3)).toBe("crit"))
})

describe("getTrafficState", () => {
	const now = Date.UTC(2026, 2, 1)
	test("basic usage", () => {
		const s = getTrafficState(1000, 500, Date.UTC(2026, 2, 19), now)
		expect(s.pct).toBe(50)
		expect(s.remaining).toBe(500)
		expect(s.daysLeft).toBe(18)
		expect(s.exceeded).toBe(false)
	})
	test("exceeded clamps pct to 100", () => {
		const s = getTrafficState(1000, 1200, Date.UTC(2026, 2, 19), now)
		expect(s.exceeded).toBe(true)
		expect(s.pct).toBe(100)
		expect(s.remaining).toBe(0)
	})
	test("unlimited (limit 0)", () => {
		const s = getTrafficState(0, 500, 0, now)
		expect(s.pct).toBe(0)
		expect(s.exceeded).toBe(false)
		expect(s.daysLeft).toBe(0)
	})
})

describe("traffic units", () => {
	test("converts between display units and bytes", () => {
		expect(trafficToBytes(1, "GB")).toBe(1024 ** 3)
		expect(trafficToBytes(1.5, "TB")).toBe(1.5 * 1024 ** 4)
		expect(trafficFromBytes(1024 ** 3, "GB")).toBe(1)
		expect(trafficFromBytes(1536 * 1024 ** 3, "TB")).toBe(1.5)
	})
	test("non-positive / invalid input means no limit", () => {
		expect(trafficToBytes(0, "GB")).toBe(0)
		expect(trafficToBytes(-5, "GB")).toBe(0)
		expect(trafficToBytes(Number.NaN, "GB")).toBe(0)
		expect(trafficFromBytes(0, "GB")).toBe(0)
		expect(trafficFromBytes(Number.NaN, "GB")).toBe(0)
	})
	test("round trip keeps the byte value", () => {
		const bytes = trafficToBytes(2, "TB")
		expect(trafficToBytes(trafficFromBytes(bytes, "GB"), "GB")).toBe(bytes)
	})
	test("picks the largest exact unit", () => {
		expect(pickTrafficUnit(1024 ** 3)).toBe("GB")
		expect(pickTrafficUnit(1024 ** 4)).toBe("TB")
		expect(pickTrafficUnit(1024 ** 5)).toBe("PB")
		expect(pickTrafficUnit(1024 ** 2)).toBe("MB")
		// 1.5 TB has no exact TB representation, so GB is used instead.
		expect(pickTrafficUnit(1.5 * 1024 ** 4)).toBe("GB")
		expect(pickTrafficUnit(0)).toBe("GB")
	})
})

describe("getTrafficSeverity", () => {
	test("bands match the PRD (<80 ok, 80-99 warn, >=100 crit)", () => {
		expect(getTrafficSeverity(0)).toBe("ok")
		expect(getTrafficSeverity(79.9)).toBe("ok")
		expect(getTrafficSeverity(80)).toBe("warn")
		expect(getTrafficSeverity(99.9)).toBe("warn")
		expect(getTrafficSeverity(100)).toBe("crit")
		expect(getTrafficSeverity(140)).toBe("crit")
		expect(getTrafficSeverity(Number.NaN)).toBe("ok")
	})
})

describe("addCycle (must match Go AddCycle)", () => {
	test("Jan 31 + 1 month -> Feb 28 (non-leap)", () => {
		const r = addCycle(new Date(Date.UTC(2026, 0, 31)), "monthly")
		expect(r.getTime()).toBe(new Date(Date.UTC(2026, 1, 28)).getTime())
	})
	test("Jan 31 + 1 month -> Feb 29 (leap)", () => {
		const r = addCycle(new Date(Date.UTC(2024, 0, 31)), "monthly")
		expect(r.getTime()).toBe(new Date(Date.UTC(2024, 1, 29)).getTime())
	})
	test("annual keeps day-of-month", () => {
		const r = addCycle(new Date(Date.UTC(2026, 0, 15)), "annual")
		expect(r.getTime()).toBe(new Date(Date.UTC(2027, 0, 15)).getTime())
	})
	test("triennial keeps day-of-month", () => {
		const r = addCycle(new Date(Date.UTC(2026, 5, 10)), "triennial")
		expect(r.getTime()).toBe(new Date(Date.UTC(2029, 5, 10)).getTime())
	})
	test("semiannual over month-end boundary", () => {
		// Jan 31 + 6 months = Jul 31 (July has 31 days, no clamp)
		const r = addCycle(new Date(Date.UTC(2026, 0, 31)), "semiannual")
		expect(r.getTime()).toBe(new Date(Date.UTC(2026, 6, 31)).getTime())
	})
	test("permanent -> zero time", () => {
		expect(addCycle(new Date(Date.UTC(2026, 0, 1)), "permanent").getTime()).toBe(0)
	})
	test("unknown cycle -> zero time", () => {
		// @ts-expect-error exercising the default branch
		expect(addCycle(new Date(Date.UTC(2026, 0, 1)), "bogus").getTime()).toBe(0)
	})
})
