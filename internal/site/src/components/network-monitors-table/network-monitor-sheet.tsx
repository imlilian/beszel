import {
	getCertDaysLeft,
	getCertExpiryLevel,
	getMonitorDisplayName,
	getMonitorProtocolLabel,
	getMonitorTarget,
} from "@/lib/network-monitor-utils"
import { t } from "@lingui/core/macro"
import { Trans } from "@lingui/react/macro"
import {
	ArrowLeftRightIcon,
	EthernetPortIcon,
	GlobeIcon,
	LandmarkIcon,
	ServerIcon,
	ShieldCheckIcon,
} from "lucide-react"
import { useMemo, useState } from "react"
import { atom } from "nanostores"
import { useStore } from "@nanostores/react"
import { getPagePath } from "@nanostores/router"
import ChartTimeSelect from "@/components/charts/chart-time-select"
import { AvgMinMaxResponseChart, LossChart, ResponseChart } from "@/components/routes/system/charts/monitors-charts"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { getMonitorCompareState } from "@/lib/monitor-compare"
import { $allSystemsById, $direction, getUserChartTime } from "@/lib/stores"
import { useCompareMonitors, useNetworkMonitorStats } from "@/lib/use-network-monitors"
import { cn, formatShortDate, parseSemVer } from "@/lib/utils"
import type { ChartOptions, MonitorCertInfo, NetworkMonitorRecord } from "@/types"
import { $router, Link } from "../router"
import { Separator } from "../ui/separator"
import { MonitorMultiSelect, SystemMultiSelect } from "./monitor-dialog"

export function NetworkMonitorSheet({
	open,
	onOpenChange,
	monitor,
	monitors,
	includesAllSystems,
}: {
	open: boolean
	onOpenChange: (open: boolean) => void
	monitor?: NetworkMonitorRecord
	monitors: NetworkMonitorRecord[]
	includesAllSystems: boolean
}) {
	if (!monitor) {
		return null
	}

	return (
		<NetworkMonitorSheetContent
			key={monitor.system}
			open={open}
			onOpenChange={onOpenChange}
			monitor={monitor}
			monitors={monitors}
			includesAllSystems={includesAllSystems}
		/>
	)
}

const certExpiryTextColors = { ok: "", warning: "text-yellow-600 dark:text-yellow-500", critical: "text-red-500" }

function CertExpiry({ cert }: { cert: MonitorCertInfo }) {
	const daysLeft = getCertDaysLeft(cert)
	const expires = formatShortDate(new Date(cert.expires).toISOString())
	const level = getCertExpiryLevel(daysLeft)
	return (
		<>
			<Separator orientation="vertical" className="h-2.5 bg-muted-foreground opacity-70" />
			<ShieldCheckIcon className={cn("size-3.5 text-muted-foreground -me-1", certExpiryTextColors[level])} />
			<span className={certExpiryTextColors[level]}>
				{daysLeft < 0 ? <Trans>Certificate expired {expires}</Trans> : <Trans>Certificate expires {expires}</Trans>}
			</span>
			{cert.issuer && (
				<>
					<Separator orientation="vertical" className="h-2.5 bg-muted-foreground opacity-70" />
					<LandmarkIcon className="size-3.5 text-muted-foreground -me-0.5" />
					<span>{cert.issuer}</span>
				</>
			)}
		</>
	)
}

function NetworkMonitorSheetContent({
	open,
	onOpenChange,
	monitor,
	monitors,
	includesAllSystems,
}: {
	open: boolean
	onOpenChange: (open: boolean) => void
	monitor: NetworkMonitorRecord
	/** Table monitors; used to find other targets on the same system to compare against. */
	monitors: NetworkMonitorRecord[]
	/** Whether `monitors` covers every system, so other systems' monitors needn't be fetched. */
	includesAllSystems: boolean
}) {
	// Keep monitor exploration independent of the system charts' time range.
	const [chartTimeStore] = useState(() => {
		const defaultTime = getUserChartTime()
		return atom(defaultTime === "1m" ? "1h" : defaultTime)
	})
	const chartTime = useStore(chartTimeStore)
	const direction = useStore($direction)
	const systems = useStore($allSystemsById)
	const system = systems[monitor.system]

	const [compareTargetIds, setCompareTargetIds] = useState<Set<string>>(() => new Set())
	const [compareSystemIds, setCompareSystemIds] = useState<Set<string>>(() => new Set())
	// Scoped to this sheet so a filter doesn't carry over to other monitors' sheets.
	const [compareFilterStore, setCompareFilterStore] = useState(() => atom(""))
	// The sheet is keyed by system (to keep the time range), so reset comparison state per monitor.
	const [compareMonitorId, setCompareMonitorId] = useState(monitor.id)
	if (compareMonitorId !== monitor.id) {
		setCompareMonitorId(monitor.id)
		setCompareSystemIds(new Set())
		setCompareTargetIds(new Set())
		setCompareFilterStore(atom(""))
	}
	// Other systems' monitors come from the table when it lists every system, otherwise from one fetch.
	const fetchedMonitors = useCompareMonitors(monitor.system, monitor.protocol, open && !includesAllSystems)
	const compare = useMemo(
		() =>
			getMonitorCompareState({
				monitor,
				localMonitors: monitors,
				otherMonitors: includesAllSystems ? monitors : fetchedMonitors,
				selectedSystemIds: compareSystemIds,
				selectedTargetIds: compareTargetIds,
				getSystemName: (id) => systems[id]?.name ?? id,
			}),
		[monitor, monitors, includesAllSystems, fetchedMonitors, compareSystemIds, compareTargetIds, systems]
	)
	const { compareMonitors } = compare
	const comparing = compareMonitors.length > 1

	const monitorStats = useNetworkMonitorStats({
		systemId: monitor.system,
		monitorIds: comparing ? compareMonitors.map((m) => m.id) : [monitor.id],
		interval: monitor.interval,
		chartTime,
		enabled: open,
	})

	const chartData = useMemo<ChartOptions>(
		() => ({
			agentVersion: parseSemVer(system?.info?.v),
			orientation: direction === "rtl" ? "right" : "left",
			chartTime,
		}),
		[system?.info?.v, direction, chartTime]
	)
	const hasMonitorStats = comparing
		? monitorStats.some((record) => record.stats != null)
		: monitorStats.some((record) => record.stats?.[monitor.id] != null)
	const monitorLabel = getMonitorDisplayName(monitor)
	const monitorTargetLabel = getMonitorTarget(monitor)
	const hasCustomName = !!monitor.name?.trim()

	return (
		<Sheet open={open} onOpenChange={onOpenChange}>
			<SheetContent className="w-full sm:max-w-220 overflow-auto p-4 sm:p-6">
				<SheetHeader className="mb-0 border-b p-0 pb-4">
					<SheetTitle>{monitorLabel}</SheetTitle>
					<SheetDescription className="flex flex-wrap items-center gap-x-2 gap-y-1">
						<ServerIcon className="size-3.5 text-muted-foreground" />
						<Link className="hover:underline" href={getPagePath($router, "system", { id: system?.id ?? "" })}>
							{system?.name ?? ""}
						</Link>
						{hasCustomName && (
							<>
								<Separator orientation="vertical" className="h-2.5 bg-muted-foreground opacity-70" />
								<EthernetPortIcon className="size-3.5 text-muted-foreground" />
								<span>{monitorTargetLabel}</span>
							</>
						)}
						<Separator orientation="vertical" className="h-2.5 bg-muted-foreground opacity-70" />
						<ArrowLeftRightIcon className="size-3.5 text-muted-foreground -me-0.5" />
						{getMonitorProtocolLabel(monitor).toUpperCase()}
						{monitor.protocol === "tcp" && monitor.port > 0 && (
							<>
								<Separator orientation="vertical" className="h-2.5 bg-muted-foreground opacity-70" />
								<EthernetPortIcon className="size-3.5 text-muted-foreground" />
								<span>{monitor.port}</span>
							</>
						)}
						{monitor.protocol === "dns" && monitor.server && (
							<>
								<Separator orientation="vertical" className="h-2.5 bg-muted-foreground opacity-70" />
								<GlobeIcon className="size-3.5 text-muted-foreground" />
								<span>{monitor.server}</span>
							</>
						)}
						{monitor.certInfo?.expires ? <CertExpiry cert={monitor.certInfo} /> : null}
					</SheetDescription>
				</SheetHeader>
				<div className="grid gap-4">
					<div className="flex flex-wrap items-center gap-2">
						<ChartTimeSelect
							className="bg-card flex-1 min-w-0 basis-full sm:basis-0"
							agentVersion={chartData.agentVersion}
							chartTimeStore={chartTimeStore}
							allowRealtime={false}
						/>
						<MonitorMultiSelect
							id="monitor-compare-targets"
							className="flex-1 min-w-0 basis-full sm:basis-0 bg-card"
							monitors={compare.targetOptions}
							selectedMonitorIds={compare.selectedTargetIds}
							onChange={setCompareTargetIds}
							disabled={compare.targetOptions.length === 0}
							canSelectMore={compare.canAddTarget}
							placeholder={t`Compare with other targets`}
						/>
						<SystemMultiSelect
							id="monitor-compare-systems"
							className="flex-1 min-w-0 basis-full sm:basis-0 bg-card"
							systemIds={compare.systemOptions}
							selectedSystemIds={compare.selectedSystemIds}
							onChange={setCompareSystemIds}
							disabled={compare.systemOptions.length === 0}
							canSelectMore={compare.canAddSystem}
							placeholder={t`Compare with other systems`}
						/>
					</div>
					{comparing ? (
						<>
							<ResponseChart
								monitorStats={monitorStats}
								grid={false}
								monitors={compareMonitors}
								chartData={chartData}
								empty={!hasMonitorStats}
								getLabel={compare.getLabel}
								filterStore={compareFilterStore}
							/>
							<LossChart
								monitorStats={monitorStats}
								grid={false}
								monitors={compareMonitors}
								chartData={chartData}
								empty={!hasMonitorStats}
								getLabel={compare.getLabel}
								filterStore={compareFilterStore}
							/>
						</>
					) : (
						<>
							<AvgMinMaxResponseChart
								monitorStats={monitorStats}
								monitor={monitor}
								chartData={chartData}
								empty={!hasMonitorStats}
							/>
							<LossChart
								monitorStats={monitorStats}
								grid={false}
								monitors={[monitor]}
								chartData={chartData}
								empty={!hasMonitorStats}
								showFilter={false}
							/>
						</>
					)}
				</div>
			</SheetContent>
		</Sheet>
	)
}
