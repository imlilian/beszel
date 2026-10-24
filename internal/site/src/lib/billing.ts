import { atom } from "nanostores"
import { useEffect } from "react"
import type { RecordModel } from "pocketbase"
import { pb } from "@/lib/api"
import type { BillingType, Currency, Cycle, TrafficMode } from "@/lib/billing-utils"

// Types are defined once in billing-utils (the pure, test-friendly module) and
// re-exported here so components can import everything from "@/lib/billing".
export type { BillingType, Currency, Cycle, TrafficMode } from "@/lib/billing-utils"

/** Typed view of a `system_billing` record (cost + traffic-quota config). */
export interface BillingRecord extends RecordModel {
	system: string
	billingType: BillingType
	price: number
	currency: Currency
	cycle: Cycle
	paidAt: string
	expiresAt: string
	notifyDaysBefore: number
	remindLevel: number
	remindAt: string
	trafficLimit: number
	trafficMode: TrafficMode
	resetDay: number
	trafficAlerts: boolean
	trafficRemindLevel: number
	manualResetAt: string
}

/** Typed view of a `system_traffic` record (runtime counters, hub-written). */
export interface TrafficRecord extends RecordModel {
	system: string
	periodStart: string
	periodEnd: string
	usedUpload: number
	usedDownload: number
	/** Excluded from TRAFFIC_FIELDS (a per-interface counters blob); absent in UI reads. */
	lastCounters?: Record<string, [number, number]>
	/** Excluded from TRAFFIC_FIELDS; absent in UI reads. */
	lastSampleAt?: string
	lastResetAt: string
	resetReason: string
}

/** systemId -> billing config. Empty until useBillingSync() runs. */
export const $billingBySystemId = atom<Record<string, BillingRecord>>({})
/** systemId -> traffic counters. Empty until useBillingSync() runs. */
export const $trafficBySystemId = atom<Record<string, TrafficRecord>>({})

// The UI reads the cost/traffic fields; the reminder bookkeeping is cheap and
// kept so the BillingRecord type stays honest for every fetched record.
const BILLING_FIELDS =
	"system,billingType,price,currency,cycle,paidAt,expiresAt,notifyDaysBefore,remindLevel,remindAt,trafficLimit,trafficMode,resetDay,trafficAlerts,trafficRemindLevel,manualResetAt"
// `lastCounters` (per-interface blob) and `lastSampleAt` are intentionally not
// fetched; they are optional on TrafficRecord and unused by the UI.
const TRAFFIC_FIELDS = "system,periodStart,periodEnd,usedUpload,usedDownload,lastResetAt,resetReason"

/**
 * Fetch every billing config + traffic counter once, then keep both stores in
 * sync via realtime subscriptions. Billing changes are rare (human edits), so a
 * 50ms batch is plenty; traffic counters update every minute per system, so they
 * are throttled to 300ms to avoid re-rendering the whole card wall on every tick.
 *
 * Call this once from the systems table (it is idempotent across remounts via the
 * effect cleanup). For personal-scale deployments all records are kept in memory;
 * a per-visible-system window is a future optimization (TODO).
 */
export function useBillingSync(enabled = true) {
	useEffect(() => {
		if (!enabled) return
		let cancelled = false
		let unsubBilling: (() => void) | undefined
		let unsubTraffic: (() => void) | undefined

		const pendingBilling = new Map<string, { action: string; record: BillingRecord }>()
		let billingTimer: ReturnType<typeof setTimeout> | null = null
		const pendingTraffic = new Map<string, { action: string; record: TrafficRecord }>()
		let trafficTimer: ReturnType<typeof setTimeout> | null = null

		const flushBilling = () => {
			billingTimer = null
			if (!pendingBilling.size) return
			const events = Array.from(pendingBilling.values())
			pendingBilling.clear()
			const next = { ...$billingBySystemId.get() }
			for (const { action, record } of events) {
				if (action === "delete") delete next[record.system]
				else next[record.system] = record
			}
			$billingBySystemId.set(next)
		}

		const flushTraffic = () => {
			trafficTimer = null
			if (!pendingTraffic.size) return
			const events = Array.from(pendingTraffic.values())
			pendingTraffic.clear()
			const next = { ...$trafficBySystemId.get() }
			for (const { action, record } of events) {
				if (action === "delete") delete next[record.system]
				else next[record.system] = record
			}
			$trafficBySystemId.set(next)
		}

		;(async () => {
			try {
				const [billing, traffic] = await Promise.all([
					pb.collection<BillingRecord>("system_billing").getFullList({ sort: "+system", fields: BILLING_FIELDS }),
					pb.collection<TrafficRecord>("system_traffic").getFullList({ sort: "+system", fields: TRAFFIC_FIELDS }),
				])
				if (cancelled) return
				const bMap: Record<string, BillingRecord> = {}
				for (const r of billing) bMap[r.system] = r
				$billingBySystemId.set({ ...$billingBySystemId.get(), ...bMap })
				const tMap: Record<string, TrafficRecord> = {}
				for (const r of traffic) tMap[r.system] = r
				$trafficBySystemId.set({ ...$trafficBySystemId.get(), ...tMap })
			} catch (error) {
				// Non-fatal: the collections may be empty or temporarily unavailable.
				console.error("Failed to load billing data:", error)
			}

			try {
				unsubBilling = await pb.collection<BillingRecord>("system_billing").subscribe(
					"*",
					(event) => {
						pendingBilling.set(event.record.system, { action: event.action, record: event.record })
						if (!billingTimer) billingTimer = setTimeout(flushBilling, 50)
					},
					{ fields: BILLING_FIELDS }
				)
			} catch (error) {
				console.error("Failed to subscribe to system_billing:", error)
			}

			try {
				unsubTraffic = await pb.collection<TrafficRecord>("system_traffic").subscribe(
					"*",
					(event) => {
						pendingTraffic.set(event.record.system, { action: event.action, record: event.record })
						if (!trafficTimer) trafficTimer = setTimeout(flushTraffic, 300)
					},
					{ fields: TRAFFIC_FIELDS }
				)
			} catch (error) {
				console.error("Failed to subscribe to system_traffic:", error)
			}
		})()

		return () => {
			cancelled = true
			if (billingTimer) clearTimeout(billingTimer)
			if (trafficTimer) clearTimeout(trafficTimer)
			unsubBilling?.()
			unsubTraffic?.()
		}
	}, [enabled])
}
