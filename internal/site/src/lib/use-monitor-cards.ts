import { pb } from "@/lib/api"
import {
	BLOCK_COUNT,
	alignNow,
	buildQualityBlocks,
	getBlockSpec,
	type QualityBlock,
} from "@/lib/monitor-blocks"
import type { MonitorThresholds } from "@/lib/monitor-thresholds"
import { monitorIdsFilter } from "@/lib/use-network-monitors"
import type { NetworkMonitorRecord, RawMonitorStatsRecord } from "@/types"
import { useEffect, useMemo, useRef, useState } from "react"

const STATS_FIELDS = "monitor,res_min,res_max,total_count,success_count,res_sum,created,type"

/** Records only exist for these two aggregates; everything else is unexpected. */
type StatsType = "1m" | "10m"

const STATS_TYPES: StatsType[] = ["1m", "10m"]

/** A `getFullList` filter with more ids than this gets truncated to keep URLs sane. */
const MAX_MONITORS_PER_REQUEST = 120

const REALTIME_FLUSH_MS = 200

type StatsByType = Record<StatsType, RawMonitorStatsRecord[]>

const EMPTY_STATS: StatsByType = { "1m": [], "10m": [] }

interface UseMonitorCardStatsProps {
	/** Monitors currently visible (already filtered and paginated by the caller). */
	monitors: NetworkMonitorRecord[]
	thresholds: MonitorThresholds
}

/** Unique key for a raw record, used to dedupe fetched rows against realtime ones. */
function recordKey(record: RawMonitorStatsRecord) {
	return `${record.type}:${record.monitor}:${record.created}`
}

/** Merge two raw record lists, newest last, dropping duplicates. */
function mergeRecords(...lists: RawMonitorStatsRecord[][]) {
	const byKey = new Map<string, RawMonitorStatsRecord>()
	for (const list of lists) {
		for (const record of list) {
			byKey.set(recordKey(record), record)
		}
	}
	return Array.from(byKey.values()).sort((a, b) => a.created - b.created)
}

/** Drop records that can no longer fall inside the widest window, so the buffer cannot grow forever. */
function pruneRecords(records: RawMonitorStatsRecord[], widestBlockMs: number) {
	const from = alignNow(Date.now(), widestBlockMs) - widestBlockMs * BLOCK_COUNT
	return records.filter((record) => record.created > from)
}

/**
 * Quality blocks for a wall of monitor cards.
 *
 * Deliberately does **one** request per record type (at most two) plus **one** realtime
 * subscription, no matter how many monitors are visible. Never call `useNetworkMonitorStats`
 * per card: that would mean N queries and N subscriptions.
 *
 * Returns `monitorId -> blocks` (newest block first).
 */
export function useMonitorCardStats({ monitors, thresholds }: UseMonitorCardStatsProps): Map<string, QualityBlock[]> {
	// Group by the block size the monitor needs, and keep a stable dependency key so effects
	// don't re-run just because the caller passed a new array with the same monitors.
	const groups = useMemo(() => {
		const grouped: Record<StatsType, NetworkMonitorRecord[]> = { "1m": [], "10m": [] }
		for (const monitor of monitors) {
			grouped[getBlockSpec(monitor).type].push(monitor)
		}
		return grouped
	}, [monitors])
	const idsKey = `${groups["1m"].map((m) => m.id).join(",")}|${groups["10m"].map((m) => m.id).join(",")}`

	const monitorsRef = useRef(monitors)
	monitorsRef.current = monitors

	const [records, setRecords] = useState<StatsByType>(EMPTY_STATS)
	const [nowMs, setNowMs] = useState(() => Date.now())

	// Initial history: one request per record type.
	useEffect(() => {
		let cancelled = false
		const now = Date.now()
		const fetched: StatsByType = { "1m": [], "10m": [] }
		const requests: Promise<void>[] = []

		for (const type of STATS_TYPES) {
			const list = groups[type].slice(0, MAX_MONITORS_PER_REQUEST)
			if (!list.length) continue
			const spec = getBlockSpec(list[0])
			const from = alignNow(now, spec.blockMs) - spec.blockMs * BLOCK_COUNT
			const { expr, params } = monitorIdsFilter(list.map((monitor) => monitor.id))
			const thisType: StatsType = type
			requests.push(
				pb
					.collection<RawMonitorStatsRecord>("network_monitor_stats")
					.getFullList({
						filter: pb.filter(`${expr} && type={:type} && created>{:from}`, {
							...params,
							type: thisType,
							from,
						}),
						fields: STATS_FIELDS,
						sort: "created",
					})
					.then((result) => {
						fetched[thisType] = result
					})
					.catch((error) => {
						console.error("Failed to fetch monitor card stats:", error)
					})
			)
		}

		Promise.all(requests).then(() => {
			if (cancelled) return
			// Realtime records may already have arrived while the requests were in flight.
			setRecords((prev) => ({
				"1m": mergeRecords(fetched["1m"], prev["1m"]),
				"10m": mergeRecords(fetched["10m"], prev["10m"]),
			}))
		})

		return () => {
			cancelled = true
		}
	}, [idsKey])

	// Single realtime subscription, split by record type on arrival.
	useEffect(() => {
		let cancelled = false
		let unsubscribe: (() => void) | undefined
		const pending = new Map<string, RawMonitorStatsRecord>()
		let flushTimeout: ReturnType<typeof setTimeout> | null = null

		function flush() {
			flushTimeout = null
			if (!pending.size) return
			const batch = Array.from(pending.values())
			pending.clear()

			// Only keep records for monitors on screen, and only the type each monitor reads.
			const expectedType = new Map<string, StatsType>()
			for (const monitor of monitorsRef.current) {
				expectedType.set(monitor.id, getBlockSpec(monitor).type)
			}
			const accepted: StatsByType = { "1m": [], "10m": [] }
			for (const record of batch) {
				if (record.type !== "1m" && record.type !== "10m") continue
				if (expectedType.get(record.monitor) !== record.type) continue
				accepted[record.type].push(record)
			}
			if (!accepted["1m"].length && !accepted["10m"].length) return

			setRecords((prev) => ({
				"1m": pruneRecords(mergeRecords(prev["1m"], accepted["1m"]), 2 * 60_000),
				"10m": pruneRecords(mergeRecords(prev["10m"], accepted["10m"]), 10 * 60_000),
			}))
		}

		;(async () => {
			try {
				unsubscribe = await pb.collection<RawMonitorStatsRecord>("network_monitor_stats").subscribe(
					"*",
					(event) => {
						if (cancelled || event.action !== "create") return
						const record = event.record
						pending.set(recordKey(record), record)
						if (!flushTimeout) {
							flushTimeout = setTimeout(flush, REALTIME_FLUSH_MS)
						}
					},
					{ fields: STATS_FIELDS }
				)
				if (cancelled) unsubscribe()
			} catch (error) {
				console.error("Failed to subscribe to monitor card stats:", error)
			}
		})()

		return () => {
			cancelled = true
			if (flushTimeout) {
				clearTimeout(flushTimeout)
				flushTimeout = null
			}
			pending.clear()
			unsubscribe?.()
		}
	}, [])

	// Shift the bar on block boundaries instead of on every render, and pause while hidden.
	useEffect(() => {
		const tick = () => {
			if (!document.hidden) setNowMs(Date.now())
		}
		const interval = setInterval(tick, 60_000)
		document.addEventListener("visibilitychange", tick)
		return () => {
			clearInterval(interval)
			document.removeEventListener("visibilitychange", tick)
		}
	}, [])

	// Blocks per monitor. Cache by monitor so an unrelated update keeps the same array
	// identity and the memoized block strips skip re-rendering.
	const cache = useRef(new Map<string, { key: string; blocks: QualityBlock[] }>())

	return useMemo(() => {
		const recordsByMonitor = new Map<string, RawMonitorStatsRecord[]>()
		for (const type of STATS_TYPES) {
			for (const record of records[type]) {
				const list = recordsByMonitor.get(record.monitor)
				if (list) {
					list.push(record)
				} else {
					recordsByMonitor.set(record.monitor, [record])
				}
			}
		}

		const result = new Map<string, QualityBlock[]>()
		const seen = new Set<string>()

		for (const monitor of monitors) {
			const spec = getBlockSpec(monitor)
			const list = recordsByMonitor.get(monitor.id) ?? []
			const lastCreated = list.length ? list[list.length - 1].created : 0
			const key = `${spec.type}:${alignNow(nowMs, spec.blockMs)}:${lastCreated}:${list.length}:${thresholds.warnMs}:${thresholds.critMs}`
			let entry = cache.current.get(monitor.id)
			if (!entry || entry.key !== key) {
				entry = { key, blocks: buildQualityBlocks(list, spec, nowMs, thresholds) }
				cache.current.set(monitor.id, entry)
			}
			seen.add(monitor.id)
			result.set(monitor.id, entry.blocks)
		}

		for (const id of cache.current.keys()) {
			if (!seen.has(id)) cache.current.delete(id)
		}

		return result
	}, [monitors, records, nowMs, thresholds])
}
