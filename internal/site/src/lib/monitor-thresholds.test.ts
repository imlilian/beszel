import { expect, test } from "bun:test"
import {
	DEFAULT_MONITOR_THRESHOLDS,
	getMonitorLevel,
	getMonitorThresholds,
	monitorLevelBg,
	monitorLevelText,
} from "./monitor-thresholds"

test("defaults when settings are missing", () => {
	expect(getMonitorThresholds()).toEqual(DEFAULT_MONITOR_THRESHOLDS)
	expect(getMonitorThresholds(null)).toEqual(DEFAULT_MONITOR_THRESHOLDS)
	expect(getMonitorThresholds({})).toEqual(DEFAULT_MONITOR_THRESHOLDS)
})

test("coerces numbers submitted as form strings", () => {
	// FormData hands back strings, so the settings UI stores them that way.
	expect(getMonitorThresholds({ monitorWarnMs: "200" as unknown as number })).toEqual({ warnMs: 200, critMs: 400 })
	expect(getMonitorThresholds({ monitorWarnMs: 200, monitorCritMs: 600 })).toEqual({ warnMs: 200, critMs: 600 })
})

test("falls back on unparsable or non-positive values", () => {
	expect(getMonitorThresholds({ monitorWarnMs: "abc" as unknown as number }).warnMs).toBe(
		DEFAULT_MONITOR_THRESHOLDS.warnMs
	)
	// 0 and "" come from an emptied number input
	expect(getMonitorThresholds({ monitorWarnMs: 0 }).warnMs).toBe(DEFAULT_MONITOR_THRESHOLDS.warnMs)
	expect(getMonitorThresholds({ monitorWarnMs: "" as unknown as number }).warnMs).toBe(
		DEFAULT_MONITOR_THRESHOLDS.warnMs
	)
	expect(getMonitorThresholds({ monitorWarnMs: -50 }).warnMs).toBe(DEFAULT_MONITOR_THRESHOLDS.warnMs)
})

test("critical must stay above warning", () => {
	// Otherwise the yellow band would be unreachable.
	expect(getMonitorThresholds({ monitorWarnMs: 500, monitorCritMs: 200 })).toEqual({ warnMs: 500, critMs: 501 })
	expect(getMonitorThresholds({ monitorWarnMs: 500, monitorCritMs: 500 })).toEqual({ warnMs: 500, critMs: 501 })
})

test("classifies response times at the band boundaries", () => {
	const t = { warnMs: 150, critMs: 400 }
	expect(getMonitorLevel(0, t)).toBe("good")
	expect(getMonitorLevel(150_000, t)).toBe("good")
	expect(getMonitorLevel(150_001, t)).toBe("warn")
	expect(getMonitorLevel(400_000, t)).toBe("warn")
	expect(getMonitorLevel(400_001, t)).toBe("crit")
	// 900ms never reaches the UI as a success: the agent reports it as loss (-1).
	expect(getMonitorLevel(-1, t)).toBe("loss")
	expect(getMonitorLevel(null, t)).toBe("loss")
	expect(getMonitorLevel(undefined, t)).toBe("loss")
})

test("respects custom thresholds", () => {
	const t = { warnMs: 20, critMs: 50 }
	expect(getMonitorLevel(20_000, t)).toBe("good")
	expect(getMonitorLevel(20_001, t)).toBe("warn")
	expect(getMonitorLevel(50_001, t)).toBe("crit")
})

test("every level has both a background and a text style", () => {
	for (const level of ["good", "warn", "crit", "loss", "empty"] as const) {
		expect(monitorLevelBg[level]).toBeTruthy()
		expect(typeof monitorLevelText[level]).toBe("string")
	}
	// crit and loss must not be indistinguishable
	expect(monitorLevelBg.crit).not.toBe(monitorLevelBg.loss)
})
