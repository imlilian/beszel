import { describe, expect, test } from "bun:test"
import {
	BLOCK_COUNT,
	alignNow,
	buildQualityBlocks,
	getBlockLevel,
	getBlockSpec,
	hasPartialLoss,
	type QualityBlock,
} from "./monitor-blocks"
import { DEFAULT_MONITOR_THRESHOLDS } from "./monitor-thresholds"

const thresholds = DEFAULT_MONITOR_THRESHOLDS
const now = 1_700_000_100_000

/** A raw `network_monitor_stats` row. */
function record(created: number, total: number, success: number, sumUs = 0, minUs = 0, maxUs = 0) {
	return {
		monitor: "m1",
		created,
		total_count: total,
		success_count: success,
		res_sum: sumUs,
		res_min: minUs,
		res_max: maxUs,
	}
}

function block(overrides: Partial<QualityBlock>): QualityBlock {
	return {
		startMs: 0,
		endMs: 0,
		level: "empty",
		avgUs: null,
		minUs: null,
		maxUs: null,
		lossPct: 0,
		total: 0,
		success: 0,
		...overrides,
	}
}

describe("getBlockSpec", () => {
	test("short intervals use 2 minute blocks", () => {
		expect(getBlockSpec({ interval: 60 })).toEqual({ blockMs: 120_000, count: BLOCK_COUNT, type: "1m" })
		expect(getBlockSpec({ interval: 120 })).toEqual({ blockMs: 120_000, count: BLOCK_COUNT, type: "1m" })
	})

	test("long intervals use 10 minute blocks", () => {
		expect(getBlockSpec({ interval: 300 })).toEqual({ blockMs: 600_000, count: BLOCK_COUNT, type: "10m" })
	})
})

describe("alignNow", () => {
	test("rounds up to the next block boundary and is idempotent", () => {
		const aligned = alignNow(now, 120_000)
		expect(aligned % 120_000).toBe(0)
		expect(aligned).toBeGreaterThan(now)
		expect(alignNow(aligned, 120_000)).toBe(aligned)
		expect(alignNow(now + 1, 120_000)).toBe(aligned)
	})
})

describe("buildQualityBlocks", () => {
	const spec = getBlockSpec({ interval: 60 })
	const aligned = alignNow(now, spec.blockMs)

	test("records in the same block are summed by probe count", () => {
		const blocks = buildQualityBlocks(
			[
				record(aligned - 10_000, 1, 1, 100_000, 90_000, 110_000),
				record(aligned - 20_000, 1, 1, 200_000, 180_000, 220_000),
				record(aligned - 30_000, 1, 0),
			],
			spec,
			now,
			thresholds
		)
		expect(blocks).toHaveLength(BLOCK_COUNT)
		const latest = blocks[0]
		expect(latest.total).toBe(3)
		expect(latest.success).toBe(2)
		expect(latest.avgUs).toBe(150_000)
		expect(latest.minUs).toBe(90_000)
		expect(latest.maxUs).toBe(220_000)
		expect(latest.lossPct).toBeCloseTo(33.33, 1)
	})

	test("blocks without probes are empty, never loss", () => {
		const blocks = buildQualityBlocks([], spec, now, thresholds)
		expect(blocks).toHaveLength(BLOCK_COUNT)
		for (const b of blocks) {
			expect(b.total).toBe(0)
			expect(b.level).toBe("empty")
		}
	})

	test("newest block is index 0 and out-of-window records are ignored", () => {
		const blocks = buildQualityBlocks(
			[
				record(aligned - 1_000, 1, 1, 50_000),
				record(aligned - spec.blockMs - 1_000, 2, 2, 80_000),
				record(aligned - spec.blockMs * (BLOCK_COUNT + 1), 5, 5, 1_000),
			],
			spec,
			now,
			thresholds
		)
		expect(blocks[0].total).toBe(1)
		expect(blocks[1].total).toBe(2)
		const totals = blocks.reduce((sum, b) => sum + b.total, 0)
		expect(totals).toBe(3)
	})

	test("a block where every probe failed is loss", () => {
		const blocks = buildQualityBlocks([record(aligned - 1_000, 3, 0)], spec, now, thresholds)
		expect(blocks[0].level).toBe("loss")
		expect(blocks[0].avgUs).toBeNull()
		expect(hasPartialLoss(blocks[0])).toBe(false)
	})

	test("losing a third keeps the latency color plus a partial-loss edge", () => {
		const blocks = buildQualityBlocks([record(aligned - 1_000, 3, 2, 200_000)], spec, now, thresholds)
		expect(blocks[0].avgUs).toBe(100_000)
		expect(blocks[0].level).toBe("good")
		expect(hasPartialLoss(blocks[0])).toBe(true)
	})

	test("losing three quarters is loss without the edge", () => {
		const blocks = buildQualityBlocks([record(aligned - 1_000, 4, 1, 100_000)], spec, now, thresholds)
		expect(blocks[0].level).toBe("loss")
		expect(hasPartialLoss(blocks[0])).toBe(false)
	})
})

describe("getBlockLevel", () => {
	test("thresholds are applied to the successful-probe average", () => {
		expect(getBlockLevel(block({ total: 1, success: 1, avgUs: 150_000 }), thresholds)).toBe("good")
		expect(getBlockLevel(block({ total: 1, success: 1, avgUs: 150_001 }), thresholds)).toBe("warn")
		expect(getBlockLevel(block({ total: 1, success: 1, avgUs: 400_000 }), thresholds)).toBe("warn")
		expect(getBlockLevel(block({ total: 1, success: 1, avgUs: 400_001 }), thresholds)).toBe("crit")
		// A slow-but-successful probe that stayed under the agent's loss threshold is not loss.
		expect(getBlockLevel(block({ total: 1, success: 1, avgUs: 900_000 }), thresholds)).toBe("crit")
	})

	test("empty beats loss when nothing was probed", () => {
		expect(getBlockLevel(block({ total: 0, success: 0, avgUs: null }), thresholds)).toBe("empty")
	})
})
