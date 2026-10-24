import { memo, useCallback, useMemo } from "react"
import { Trans, useLingui } from "@lingui/react/macro"
import { getPagePath } from "@nanostores/router"
import { MoreHorizontalIcon, PauseCircleIcon, PlayCircleIcon, ServerIcon } from "lucide-react"
import { IndicatorDot } from "@/components/systems-table/systems-table-columns"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useToast } from "@/components/ui/use-toast"
import { pb } from "@/lib/api"
import { BLOCK_COUNT, type QualityBlock } from "@/lib/monitor-blocks"
import { getMonitorDisplayName, getMonitorProtocolLabel } from "@/lib/network-monitor-utils"
import { getMonitorLevel, monitorLevelText, type MonitorThresholds } from "@/lib/monitor-thresholds"
import { cn, formatMicroseconds } from "@/lib/utils"
import type { NetworkMonitorRecord, SystemRecord } from "@/types"
import { $router, navigate } from "../router"
import { AddMonitorDialog } from "./monitor-dialog"
import { QualityBlocks } from "./quality-blocks"

/** Blocks shown per system card before the rest collapse into "+N more". */
const MAX_BLOCKS_PER_CARD = 3

/** Placeholder strip for monitors whose stats have not loaded (or do not exist yet). */
const EMPTY_BLOCKS: QualityBlock[] = Array.from({ length: BLOCK_COUNT }, () => ({
	startMs: 0,
	endMs: 0,
	level: "empty" as const,
	avgUs: null,
	minUs: null,
	maxUs: null,
	lossPct: 0,
	total: 0,
	success: 0,
}))

/**
 * Container-query based grid for the blocks inside a system card.
 *
 * Narrow cards fall back to one column on purpose: a 350-420px card split in three leaves
 * ~120px per block, which cannot fit two metric rows plus a 20 segment strip.
 */
export function getMonitorGridClass(cols: number) {
	if (cols <= 1) return "grid-cols-1"
	if (cols === 2) return "grid-cols-1 @[340px]:grid-cols-2"
	return "grid-cols-1 @[340px]:grid-cols-2 @[560px]:grid-cols-3"
}

interface MonitorCardGridProps {
	/** Monitors on the current page (already filtered by the table). */
	monitors: NetworkMonitorRecord[]
	blocksByMonitor: Map<string, QualityBlock[]>
	thresholds: MonitorThresholds
	systems: Record<string, SystemRecord>
	canManage: boolean
	onOpenSheet: (monitor: NetworkMonitorRecord) => void
	/** Switch to the table view with this system's monitors filtered. */
	onShowSystemInTable: (systemId: string) => void
}

/** Card wall: one card per system, each holding up to three monitor blocks. */
export function MonitorCardGrid({
	monitors,
	blocksByMonitor,
	thresholds,
	systems,
	canManage,
	onOpenSheet,
	onShowSystemInTable,
}: MonitorCardGridProps) {
	const groups = useMemo(() => {
		const bySystem = new Map<string, NetworkMonitorRecord[]>()
		for (const monitor of monitors) {
			const list = bySystem.get(monitor.system)
			if (list) {
				list.push(monitor)
			} else {
				bySystem.set(monitor.system, [monitor])
			}
		}
		const name = (id: string) => systems[id]?.name ?? id
		return Array.from(bySystem.entries())
			.map(([systemId, list]) => ({
				systemId,
				// Worst first, so the three visible slots go to the monitors that need attention.
				monitors: list.slice().sort((a, b) => b.loss1h - a.loss1h),
			}))
			.sort((a, b) => name(a.systemId).localeCompare(name(b.systemId)))
	}, [monitors, systems])

	if (!groups.length) {
		return (
			<div className="py-8 text-center text-sm text-muted-foreground">
				<Trans>No results.</Trans>
			</div>
		)
	}

	return (
		<div className="grid gap-4 grid-cols-1 sm:grid-cols-2 lg:grid-cols-3">
			{groups.map(({ systemId, monitors: systemMonitors }) => (
				<SystemMonitorCard
					key={systemId}
					systemId={systemId}
					system={systems[systemId]}
					monitors={systemMonitors}
					blocksByMonitor={blocksByMonitor}
					thresholds={thresholds}
					canManage={canManage}
					onOpenSheet={onOpenSheet}
					onShowSystemInTable={onShowSystemInTable}
				/>
			))}
		</div>
	)
}

const SystemMonitorCard = memo(function SystemMonitorCard({
	systemId,
	system,
	monitors,
	blocksByMonitor,
	thresholds,
	canManage,
	onOpenSheet,
	onShowSystemInTable,
}: {
	systemId: string
	system?: SystemRecord
	monitors: NetworkMonitorRecord[]
	blocksByMonitor: Map<string, QualityBlock[]>
	thresholds: MonitorThresholds
	canManage: boolean
	onOpenSheet: (monitor: NetworkMonitorRecord) => void
	onShowSystemInTable: (systemId: string) => void
}) {
	const shown = monitors.slice(0, MAX_BLOCKS_PER_CARD)
	const hiddenCount = monitors.length - shown.length

	return (
		<Card className="bg-transparent dark:border-border">
			<CardHeader className="flex-row items-center justify-between gap-2 px-4 py-3 pb-2">
				<CardTitle className="flex min-w-0 items-center gap-2 text-base">
					{system ? <IndicatorDot system={system} /> : null}
					<span className="truncate">{system?.name ?? systemId}</span>
				</CardTitle>
				<div className="flex shrink-0 items-center gap-1">
					{canManage ? <AddMonitorDialog systemId={systemId} monitors={monitors} /> : null}
					<SystemCardMenu systemId={systemId} monitors={monitors} canManage={canManage} />
				</div>
			</CardHeader>
			<CardContent className="@container px-4 pb-4">
				<div className={cn("grid gap-2", getMonitorGridClass(shown.length))}>
					{shown.map((monitor) => (
						<MonitorBlock
							key={monitor.id}
							monitor={monitor}
							blocks={blocksByMonitor.get(monitor.id) ?? EMPTY_BLOCKS}
							thresholds={thresholds}
							onOpen={onOpenSheet}
						/>
					))}
				</div>
				{hiddenCount > 0 && (
					<button
						type="button"
						onClick={() => onShowSystemInTable(systemId)}
						className="mt-2 text-start text-xs text-muted-foreground transition-colors hover:text-foreground"
					>
						+{hiddenCount} <Trans>more monitors</Trans>
					</button>
				)}
			</CardContent>
		</Card>
	)
})

const MonitorBlock = memo(function MonitorBlock({
	monitor,
	blocks,
	thresholds,
	onOpen,
}: {
	monitor: NetworkMonitorRecord
	blocks: QualityBlock[]
	thresholds: MonitorThresholds
	onOpen: (monitor: NetworkMonitorRecord) => void
}) {
	const { t } = useLingui()
	// No successful probe yet: resAvg1h is 0 and loss1h is 0 (a real total loss reports 100%).
	const hasNoData = monitor.resAvg1h === 0 && monitor.loss1h === 0
	const isTotalLoss = monitor.resAvg1h === 0 && monitor.loss1h >= 100
	const level = hasNoData ? "empty" : isTotalLoss ? "loss" : getMonitorLevel(monitor.resAvg1h, thresholds)
	const latencyText = hasNoData ? "—" : isTotalLoss ? t`Loss` : formatMicroseconds(monitor.resAvg1h)

	return (
		<button
			type="button"
			onClick={() => onOpen(monitor)}
			className="w-full rounded-lg border p-2.5 text-start transition-colors hover:bg-accent/50"
		>
			<div className="flex items-center justify-between gap-2">
				<span className="truncate text-xs font-medium">{getMonitorDisplayName(monitor)}</span>
				<span className="shrink-0 rounded bg-muted px-1 py-px text-[10px] uppercase text-muted-foreground">
					{getMonitorProtocolLabel(monitor)}
				</span>
			</div>
			<div className="mt-1.5 flex items-baseline justify-between gap-2">
				<span className="text-[11px] text-muted-foreground">
					<Trans>Latency</Trans>
				</span>
				<span className={cn("text-sm font-semibold tabular-nums", monitorLevelText[level])}>{latencyText}</span>
			</div>
			<div className="flex items-baseline justify-between gap-2">
				<span className="text-[11px] text-muted-foreground">
					<Trans>Loss</Trans>
				</span>
				<span
					className={cn(
						"text-sm font-semibold tabular-nums",
						!hasNoData && monitor.loss1h > 0 ? "text-red-600 dark:text-red-400" : "text-muted-foreground"
					)}
				>
					{hasNoData ? "—" : `${monitor.loss1h.toFixed(1)}%`}
				</span>
			</div>
			<QualityBlocks blocks={blocks} className="mt-2" />
		</button>
	)
})

function SystemCardMenu({
	systemId,
	monitors,
	canManage,
}: {
	systemId: string
	monitors: NetworkMonitorRecord[]
	canManage: boolean
}) {
	const { t } = useLingui()
	const { toast } = useToast()

	const setAllEnabled = useCallback(
		async (enabled: boolean) => {
			const targets = monitors.filter((monitor) => monitor.enabled !== enabled)
			if (!targets.length) return
			try {
				await Promise.all(targets.map((monitor) => pb.collection("network_monitors").update(monitor.id, { enabled })))
			} catch (error) {
				toast({
					variant: "destructive",
					title: t`Error`,
					description: (error as Error)?.message || t`Failed to update monitors.`,
				})
			}
		},
		[monitors, t, toast]
	)

	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<Button variant="ghost" size="icon" className="size-7" aria-label={t`Monitor actions`}>
					<MoreHorizontalIcon className="size-4" />
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end">
				{canManage && (
					<>
						<DropdownMenuItem onSelect={() => setAllEnabled(false)}>
							<PauseCircleIcon className="me-2 size-4" />
							<Trans>Pause all</Trans>
						</DropdownMenuItem>
						<DropdownMenuItem onSelect={() => setAllEnabled(true)}>
							<PlayCircleIcon className="me-2 size-4" />
							<Trans>Enable all</Trans>
						</DropdownMenuItem>
						<DropdownMenuSeparator />
					</>
				)}
				<DropdownMenuItem onSelect={() => navigate(getPagePath($router, "system", { id: systemId }))}>
					<ServerIcon className="me-2 size-4" />
					<Trans>System details</Trans>
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	)
}
