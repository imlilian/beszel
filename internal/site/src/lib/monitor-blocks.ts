import type { NetworkMonitorRecord, RawMonitorStatsRecord } from "@/types"
import { getMonitorLevel, type MonitorLevel, type MonitorThresholds } from "./monitor-thresholds"

/** Number of quality blocks drawn per monitor. */
export const BLOCK_COUNT = 20

export interface BlockSpec {
	blockMs: number
	count: number
	type: "1m" | "10m"
}

/**
 * Pick the block size from the probe interval:
 * - interval <= 120s: 20 blocks x 2 minutes = 40 minute window, read from `1m` records
 *   (those are kept for one hour, so 40 minutes is safe from the cleanup task).
 * - interval > 120s: 20 blocks x 10 minutes = 200 minute window, read from `10m` records
 *   (kept for 12 hours). A 2 minute block would only hold ~8 samples in 40 minutes, leaving
 *   most of the bar grey.
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
	/** Average latency of successful probes in microseconds; null without successful probes. */
	avgUs: number | null
	minUs: number | null
	maxUs: number | null
	/** Packet loss percentage (0-100). */
	lossPct: number
	total: number
	success: number
}

/**
 * Round `now` up to the block boundary so the bar only shifts once per block instead of jittering.
 * Uses ceil rather than floor+1 so the result is idempotent: feeding an already aligned value
 * returns it unchanged, which keeps the rightmost block stable at exact boundaries.
 */
export function alignNow(nowMs: number, blockMs: number): number {
	return Math.ceil(nowMs / blockMs) * blockMs
}

/**
 * Aggregate raw stats into blocks **by probe count**, not by elapsed time.
 *
 * Returned newest-first: index 0 is the most recent block (rendered rightmost).
 * A block with `total === 0` has no probes at all (long interval, agent offline, new monitor)
 * and must stay `empty` -- it is never loss.
 */
export function buildQualityBlocks(
	records: RawMonitorStatsRecord[],
	spec: BlockSpec,
	nowMs: number,
	thresholds: MonitorThresholds
): QualityBlock[] {
	const aligned = alignNow(nowMs, spec.blockMs)
	const blocks: QualityBlock[] = []
	const sums: number[] = []

	for (let idx = 0; idx < spec.count; idx += 1) {
		blocks.push({
			startMs: aligned - (idx + 1) * spec.blockMs,
			endMs: aligned - idx * spec.blockMs,
			level: "empty",
			avgUs: null,
			minUs: null,
			maxUs: null,
			lossPct: 0,
			total: 0,
			success: 0,
		})
		sums.push(0)
	}

	for (const record of records) {
		const idx = Math.floor((aligned - record.created) / spec.blockMs)
		if (idx < 0 || idx >= spec.count) continue
		const block = blocks[idx]
		block.total += record.total_count
		block.success += record.success_count
		sums[idx] += record.res_sum
		if (record.success_count > 0) {
			block.minUs = block.minUs === null ? record.res_min : Math.min(block.minUs, record.res_min)
			block.maxUs = block.maxUs === null ? record.res_max : Math.max(block.maxUs, record.res_max)
		}
	}

	for (let idx = 0; idx < blocks.length; idx += 1) {
		const block = blocks[idx]
		if (block.success > 0) {
			block.avgUs = sums[idx] / block.success
		}
		if (block.total > 0) {
			block.lossPct = ((block.total - block.success) / block.total) * 100
		}
		block.level = getBlockLevel(block, thresholds)
	}

	return blocks
}

/** Block color, in strict priority order (PRD 6.4.3). */
export function getBlockLevel(
	block: Pick<QualityBlock, "total" | "success" | "avgUs">,
	thresholds: MonitorThresholds
): MonitorLevel {
	if (block.total === 0) return "empty" // 1. never probed -> grey
	if (block.success === 0) return "loss" // 2. every probe failed
	const lossPct = ((block.total - block.success) / block.total) * 100
	if (lossPct >= 50) return "loss" // 3. at least half lost -> unusable
	return getMonitorLevel(block.avgUs, thresholds) // 4. color by successful-probe average
}

/**
 * 0 < loss < 50%: the main color still reflects latency, and the caller adds a bottom red edge.
 * Anything at or above 50% is already fully red, so no edge is needed there.
 */
export function hasPartialLoss(block: Pick<QualityBlock, "total" | "success">): boolean {
	if (block.total === 0 || block.success === 0) return false
	const lossPct = ((block.total - block.success) / block.total) * 100
	return lossPct > 0 && lossPct < 50
}
