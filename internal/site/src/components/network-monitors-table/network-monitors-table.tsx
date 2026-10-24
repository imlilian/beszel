import { getMonitorDisplayName } from "@/lib/network-monitor-utils"
import { t } from "@lingui/core/macro"
import { Trans } from "@lingui/react/macro"
import {
	type ColumnFiltersState,
	flexRender,
	getCoreRowModel,
	getFilteredRowModel,
	getPaginationRowModel,
	getSortedRowModel,
	type Row,
	type RowSelectionState,
	type SortingState,
	type Table as TableType,
	useReactTable,
	type VisibilityState,
} from "@tanstack/react-table"
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button, buttonVariants } from "@/components/ui/button"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { subscribeKeys } from "nanostores"
import { getMonitorColumns } from "@/components/network-monitors-table/network-monitors-columns"
import { Card, CardHeader, CardTitle } from "@/components/ui/card"
import { DataTablePagination, usePagination } from "@/components/ui/data-table-pagination"
import { Input } from "@/components/ui/input"
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useToast } from "@/components/ui/use-toast"
import { isReadOnlyUser, queueUserSettings } from "@/lib/api"
import { pb } from "@/lib/api"
import { SystemStatus } from "@/lib/enums"
import { $allSystemsById, $userSettings } from "@/lib/stores"
import { cn, matchesFilterGroups, parseFilterGroups } from "@/lib/utils"
import type { NetworkMonitorRecord } from "@/types"
import { AddMonitorDialog, EditMonitorDialog } from "./monitor-dialog"
import {
	ArrowDownIcon,
	ArrowUpDownIcon,
	ArrowUpIcon,
	EyeIcon,
	LayoutGridIcon,
	LayoutListIcon,
	LoaderCircleIcon,
	Settings2Icon,
	XIcon,
} from "lucide-react"
import {
	DropdownMenu,
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuRadioGroup,
	DropdownMenuRadioItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useStore } from "@nanostores/react"
import { useMonitorCardStats } from "@/lib/use-monitor-cards"
import { getMonitorThresholds } from "@/lib/monitor-thresholds"
import { MonitorCardGrid } from "./monitor-card-grid"
import { NetworkMonitorSheet } from "./network-monitor-sheet"

type MonitorViewMode = "table" | "grid"

export default function NetworkMonitorsTableNew({
	systemId,
	monitors,
	isLoading,
}: {
	systemId?: string
	monitors: NetworkMonitorRecord[]
	isLoading: boolean
}) {
	const sortSettingsKey = systemId ? "monitorSortModeSystem" : "monitorSortMode"
	const sortStorageKey = `besz-sort-np-target-${systemId ? 1 : 0}`
	const [sorting, setSorting] = useState<SortingState>(
		() =>
			$userSettings.get()[sortSettingsKey] ??
			JSON.parse(sessionStorage.getItem(sortStorageKey) || "null") ?? [
				{ id: systemId ? "target" : "system", desc: false },
			]
	)
	const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])
	const [columnVisibility, setColumnVisibility] = useState<VisibilityState>(
		() => $userSettings.get().monitorCols ?? JSON.parse(localStorage.getItem("besz-monitor-cols") || "{}")
	)
	const [rowSelection, setRowSelection] = useState<RowSelectionState>({})
	const [globalFilter, setGlobalFilter] = useState("")
	const { pagination, onPaginationChange, resetPageIndex } = usePagination()
	const [deleteOpen, setDeleteOpen] = useState(false)
	const [pendingDelete, setPendingDelete] = useState<NetworkMonitorRecord[]>([])
	const [editingMonitor, setEditingMonitor] = useState<NetworkMonitorRecord>()
	const [viewMode, setViewMode] = useState<MonitorViewMode>(
		() =>
			$userSettings.get().monitorViewMode ??
			(JSON.parse(localStorage.getItem("besz-monitor-view") || "null") as MonitorViewMode | null) ??
			"table"
	)
	// The sheet is shared by both views, so its state lives here rather than in either one.
	const [sheetOpen, setSheetOpen] = useState(false)
	const [activeMonitorId, setActiveMonitorId] = useState<string | null>(null)
	const settings = useStore($userSettings, { keys: ["monitorWarnMs", "monitorCritMs"] })
	const systems = useStore($allSystemsById)
	const thresholds = useMemo(
		() => getMonitorThresholds(settings),
		[settings.monitorWarnMs, settings.monitorCritMs]
	)

	const { toast } = useToast()
	const canManageMonitors = !isReadOnlyUser()

	// Apply settings from server once they load (handles incognito / new devices)
	const appliedSettings = useRef(new Set<string>())
	useEffect(() => {
		return subscribeKeys($userSettings, ["monitorCols", sortSettingsKey, "monitorViewMode"], (vals) => {
			if (!appliedSettings.current.has("monitorCols") && vals.monitorCols !== undefined) {
				appliedSettings.current.add("monitorCols")
				setColumnVisibility(vals.monitorCols)
			}
			if (!appliedSettings.current.has(sortSettingsKey) && vals[sortSettingsKey] !== undefined) {
				appliedSettings.current.add(sortSettingsKey)
				setSorting(vals[sortSettingsKey] as SortingState)
			}
			if (!appliedSettings.current.has("monitorViewMode") && vals.monitorViewMode !== undefined) {
				appliedSettings.current.add("monitorViewMode")
				setViewMode(vals.monitorViewMode)
			}
		})
	}, [sortSettingsKey])

	const handleColumnVisibilityChange = useCallback(
		(updater: VisibilityState | ((prev: VisibilityState) => VisibilityState)) => {
			setColumnVisibility((prev) => {
				const next = typeof updater === "function" ? updater(prev) : updater
				localStorage.setItem("besz-monitor-cols", JSON.stringify(next))
				$userSettings.setKey("monitorCols", next)
				queueUserSettings({ monitorCols: next })
				return next
			})
		},
		[]
	)

	const handleSortingChange = useCallback(
		(updater: SortingState | ((prev: SortingState) => SortingState)) => {
			setSorting((prev) => {
				const next = typeof updater === "function" ? updater(prev) : updater
				sessionStorage.setItem(sortStorageKey, JSON.stringify(next))
				$userSettings.setKey(sortSettingsKey, next)
				queueUserSettings({ [sortSettingsKey]: next })
				return next
			})
			resetPageIndex()
		},
		[sortSettingsKey, sortStorageKey, resetPageIndex]
	)

	const handleViewModeChange = useCallback((value: string) => {
		const next = value as MonitorViewMode
		setViewMode(next)
		localStorage.setItem("besz-monitor-view", JSON.stringify(next))
		$userSettings.setKey("monitorViewMode", next)
		queueUserSettings({ monitorViewMode: next })
	}, [])

	const runMonitorBatch = useCallback(
		async (ids: string[], enqueue: (batch: ReturnType<typeof pb.createBatch>, id: string) => void) => {
			let batch = pb.createBatch()
			let inBatch = 0
			for (const id of ids) {
				enqueue(batch, id)
				if (++inBatch >= 20) {
					await batch.send()
					batch = pb.createBatch()
					inBatch = 0
				}
			}
			if (inBatch) {
				await batch.send()
			}
		},
		[]
	)

	const handleDeleteRequest = useCallback((monitorsToDelete: NetworkMonitorRecord[]) => {
		if (!monitorsToDelete.length) {
			return
		}
		setPendingDelete(monitorsToDelete)
		setDeleteOpen(true)
	}, [])

	const handleConfirmDelete = async () => {
		setDeleteOpen(false)
		const ids = pendingDelete.map((monitor) => monitor.id)
		if (!ids.length) {
			return
		}

		try {
			if (ids.length === 1) {
				await pb.collection("network_monitors").delete(ids[0])
			} else {
				await runMonitorBatch(ids, (batch, id) => batch.collection("network_monitors").delete(id))
			}
			setPendingDelete([])
			setRowSelection({})
		} catch (err: unknown) {
			toast({
				variant: "destructive",
				title: t`Error`,
				description: (err as Error)?.message || t`Failed to delete monitors.`,
			})
		}
	}

	const handleSetEnabled = useCallback(
		async (monitorsToUpdate: NetworkMonitorRecord[], enabled: boolean) => {
			if (!monitorsToUpdate.length) {
				return
			}

			const pendingUpdates = monitorsToUpdate.filter((monitor) => monitor.enabled !== enabled)
			if (!pendingUpdates.length) {
				return
			}

			try {
				if (pendingUpdates.length === 1) {
					await pb.collection("network_monitors").update(pendingUpdates[0].id, { enabled })
					return
				}
				await runMonitorBatch(
					pendingUpdates.map((monitor) => monitor.id),
					(batch, id) => batch.collection("network_monitors").update(id, { enabled })
				)
				if (monitorsToUpdate.length > 1) {
					setRowSelection({})
				}
			} catch (err: unknown) {
				toast({
					variant: "destructive",
					title: t`Error`,
					description: (err as Error)?.message || t`Failed to update monitors.`,
				})
			}
		},
		[runMonitorBatch, toast]
	)

	const columns = useMemo(() => {
		let columns = getMonitorColumns({
			onEdit: setEditingMonitor,
			onDelete: handleDeleteRequest,
			onSetEnabled: handleSetEnabled,
		})
		columns = systemId ? columns.filter((col) => col.id !== "system") : columns
		columns = canManageMonitors ? columns : columns.filter((col) => col.id !== "actions")
		return columns
	}, [canManageMonitors, handleDeleteRequest, handleSetEnabled, systemId])

	const table = useReactTable({
		data: monitors,
		columns,
		getRowId: (row) => row.id,
		getCoreRowModel: getCoreRowModel(),
		getSortedRowModel: getSortedRowModel(),
		getFilteredRowModel: getFilteredRowModel(),
		getPaginationRowModel: getPaginationRowModel(),
		autoResetPageIndex: false,
		onPaginationChange,
		onSortingChange: handleSortingChange,
		onColumnFiltersChange: setColumnFilters,
		onColumnVisibilityChange: handleColumnVisibilityChange,
		onRowSelectionChange: setRowSelection,
		defaultColumn: {
			sortUndefined: "last",
			size: 900,
			minSize: 0,
		},
		state: {
			sorting,
			columnFilters,
			columnVisibility,
			rowSelection,
			globalFilter,
			pagination,
		},
		onGlobalFilterChange: (value) => {
			setGlobalFilter(value)
			resetPageIndex()
		},
		globalFilterFn: (row, _columnId, filterValue) => {
			const value = (filterValue as string).trim()
			if (!value) return true
			const monitor = row.original
			const systemName = $allSystemsById.get()[monitor.system]?.name ?? ""
			// Include the raw target so searches for the hidden URL scheme still match,
			// and the custom name so a monitor can be found by its label.
			const searchString =
				`${getMonitorDisplayName(monitor)}${monitor.name ?? ""}${monitor.target}${monitor.protocol}${systemName}`.toLocaleLowerCase()
			return matchesFilterGroups(searchString, parseFilterGroups(value))
		},
	})

	const rows = table.getRowModel().rows
	const visibleColumns = table.getVisibleLeafColumns()
	const visibleColumnsKey = visibleColumns.map((column) => column.id).join(",")

	const cardMonitors = useMemo(() => rows.map((row) => row.original), [rows])
	const blocksByMonitor = useMonitorCardStats({
		monitors: cardMonitors,
		thresholds,
		enabled: viewMode === "grid",
	})
	const activeMonitor = activeMonitorId ? monitors.find((monitor) => monitor.id === activeMonitorId) : undefined
	const openSheet = useCallback((monitor: NetworkMonitorRecord) => {
		setActiveMonitorId(monitor.id)
		setSheetOpen(true)
	}, [])
	// "+N more" jumps to the table view with this system's monitors filtered.
	const showSystemInTable = useCallback(
		(systemId: string) => {
			handleViewModeChange("table")
			const name = $allSystemsById.get()[systemId]?.name
			if (name) {
				table.setGlobalFilter(name)
			}
			resetPageIndex()
		},
		[handleViewModeChange, resetPageIndex, table]
	)

	return (
		<Card className="@container w-full px-3 py-5 sm:py-6 sm:px-6">
			<CardHeader className="p-0 mb-3 sm:mb-4">
				<div className="grid md-lg:flex gap-x-5 gap-y-3 w-full items-end">
					<div className="px-2 sm:px-1">
						<CardTitle className="mb-2">
							<Trans>Network Monitors</Trans>
						</CardTitle>
						<div className="text-sm text-muted-foreground flex items-center flex-wrap">
							<Trans>Response time monitoring from agents.</Trans>
						</div>
					</div>
					<div className="md-lg:ms-auto flex items-center gap-2">
						{monitors.length > 0 && (
							<div className="relative grow">
								<Input
									placeholder={t`Filter...`}
									value={globalFilter}
									onChange={(e) => table.setGlobalFilter(e.target.value)}
									className="ms-auto px-4 w-full max-w-full md-lg:w-50"
								/>
								{globalFilter && (
									<Button
										type="button"
										variant="ghost"
										size="icon"
										aria-label={t`Clear`}
										className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7 text-muted-foreground"
										onClick={() => table.setGlobalFilter("")}
									>
										<XIcon className="h-4 w-4" />
									</Button>
								)}
							</div>
						)}
						<DropdownMenu>
							<DropdownMenuTrigger asChild>
								<Button aria-label={t`View`} variant="outline" size="icon">
									<Settings2Icon className="size-4 opacity-80" />
								</Button>
							</DropdownMenuTrigger>
							<DropdownMenuContent className="h-72 md:h-auto min-w-48 md:min-w-auto overflow-y-auto">
								<div
									className={cn(
										"grid divide-y md:divide-s md:divide-y-0",
										viewMode === "table" ? "grid-cols-3" : "grid-cols-1"
									)}
								>
									<div className="border-r">
										<DropdownMenuLabel className="pt-2 px-3.5 flex items-center gap-2">
											<LayoutGridIcon className="size-4" />
											<Trans>Layout</Trans>
										</DropdownMenuLabel>
										<DropdownMenuSeparator />
										<DropdownMenuRadioGroup
											className="px-1 pb-1"
											value={viewMode}
											onValueChange={handleViewModeChange}
										>
											<DropdownMenuRadioItem value="table" onSelect={(e) => e.preventDefault()} className="gap-2">
												<LayoutListIcon className="size-4" />
												<Trans>Table</Trans>
											</DropdownMenuRadioItem>
											<DropdownMenuRadioItem value="grid" onSelect={(e) => e.preventDefault()} className="gap-2">
												<LayoutGridIcon className="size-4" />
												<Trans>Grid</Trans>
											</DropdownMenuRadioItem>
										</DropdownMenuRadioGroup>
									</div>
									{viewMode === "table" && (
										<>
											<div className="border-r">
												<DropdownMenuLabel className="pt-2 px-3.5 flex items-center gap-2">
													<ArrowUpDownIcon className="size-4" />
													<Trans>Sort By</Trans>
												</DropdownMenuLabel>
												<DropdownMenuSeparator />
												<div className="px-1 pb-1">
													{table.getAllColumns().map((column) => {
														if (!column.getCanSort()) return null
														let Icon = <span className="w-6"></span>
														if (sorting[0]?.id === column.id) {
															Icon = sorting[0]?.desc ? (
																<ArrowUpIcon className="me-2 size-4" />
															) : (
																<ArrowDownIcon className="me-2 size-4" />
															)
														}
														return (
															<DropdownMenuItem
																onSelect={(e) => {
																	e.preventDefault()
																	handleSortingChange([
																		{ id: column.id, desc: sorting[0]?.id === column.id && !sorting[0]?.desc },
																	])
																}}
																key={column.id}
															>
																{Icon}
																{column.columnDef.meta?.label ?? column.id}
															</DropdownMenuItem>
														)
													})}
												</div>
											</div>
											<div>
												<DropdownMenuLabel className="pt-2 px-3.5 flex items-center gap-2">
													<EyeIcon className="size-4" />
													<Trans>Visible Fields</Trans>
												</DropdownMenuLabel>
												<DropdownMenuSeparator />
												<div className="px-1.5 pb-1">
													{table
														.getAllColumns()
														.filter((column) => column.getCanHide())
														.map((column) => (
															<DropdownMenuCheckboxItem
																key={column.id}
																onSelect={(e) => e.preventDefault()}
																checked={column.getIsVisible()}
																onCheckedChange={(value) => column.toggleVisibility(!!value)}
															>
																{column.columnDef.meta?.label ?? column.id}
															</DropdownMenuCheckboxItem>
														))}
												</div>
											</div>
										</>
									)}
								</div>
							</DropdownMenuContent>
						</DropdownMenu>
						{canManageMonitors ? <AddMonitorDialog systemId={systemId} monitors={monitors} /> : null}
						{canManageMonitors ? (
							<EditMonitorDialog
								systemId={systemId}
								monitor={editingMonitor}
								open={!!editingMonitor}
								setOpen={(open) => {
									if (!open) {
										setEditingMonitor(undefined)
									}
								}}
							/>
						) : null}
						<AlertDialog
							open={deleteOpen}
							onOpenChange={(open) => {
								setDeleteOpen(open)
								if (!open) {
									setPendingDelete([])
								}
							}}
						>
							<AlertDialogContent>
								<AlertDialogHeader>
									<AlertDialogTitle>
										<Trans>Are you sure?</Trans>
									</AlertDialogTitle>
									<AlertDialogDescription>
										<Trans>This will permanently delete all selected records from the database.</Trans>
									</AlertDialogDescription>
								</AlertDialogHeader>
								<AlertDialogFooter>
									<AlertDialogCancel>
										<Trans>Cancel</Trans>
									</AlertDialogCancel>
									<AlertDialogAction
										className={cn(buttonVariants({ variant: "destructive" }))}
										onClick={handleConfirmDelete}
									>
										<Trans>Continue</Trans>
									</AlertDialogAction>
								</AlertDialogFooter>
							</AlertDialogContent>
						</AlertDialog>
					</div>
				</div>
			</CardHeader>
			{viewMode === "table" ? (
				<div className="rounded-md">
					<NetworkMonitorsTable
						table={table}
						rows={rows}
						colLength={visibleColumns.length}
						visibleColumnsKey={visibleColumnsKey}
						rowSelection={rowSelection}
						isLoading={isLoading}
						openSheet={openSheet}
					/>
				</div>
			) : isLoading ? (
				<div className="py-8 text-center">
					<LoaderCircleIcon className="animate-spin size-10 opacity-60 mx-auto" />
				</div>
			) : (
				<MonitorCardGrid
					monitors={cardMonitors}
					blocksByMonitor={blocksByMonitor}
					thresholds={thresholds}
					systems={systems}
					canManage={canManageMonitors}
					onOpenSheet={openSheet}
					onShowSystemInTable={showSystemInTable}
				/>
			)}
			<DataTablePagination table={table} />
			<NetworkMonitorSheet
				open={sheetOpen}
				onOpenChange={setSheetOpen}
				monitor={activeMonitor}
				monitors={monitors}
				includesAllSystems={!systemId}
			/>
		</Card>
	)
}

const NetworkMonitorsTable = memo(function NetworkMonitorTable({
	table,
	rows,
	colLength,
	visibleColumnsKey,
	rowSelection,
	isLoading,
	openSheet,
}: {
	table: TableType<NetworkMonitorRecord>
	rows: Row<NetworkMonitorRecord>[]
	colLength: number
	visibleColumnsKey: string
	rowSelection: RowSelectionState
	isLoading: boolean
	openSheet: (monitor: NetworkMonitorRecord) => void
}) {
	return (
		<div className="max-w-full relative overflow-auto border rounded-md">
			<table className="text-sm w-full h-full text-nowrap">
				<NetworkMonitorTableHead table={table} />
				<TableBody>
					{rows.length ? (
						rows.map((row) => {
							return (
								<NetworkMonitorTableRow
									key={row.id}
									row={row}
									isSelected={row.getIsSelected()}
									rowSelection={rowSelection}
									visibleColumnsKey={visibleColumnsKey}
									openSheet={openSheet}
								/>
							)
						})
					) : (
						<TableRow>
							<TableCell colSpan={colLength} className="h-37 text-center pointer-events-none">
								{isLoading ? (
									<LoaderCircleIcon className="animate-spin size-10 opacity-60 mx-auto" />
								) : (
									<Trans>No results.</Trans>
								)}
							</TableCell>
						</TableRow>
					)}
				</TableBody>
			</table>
		</div>
	)
})

function NetworkMonitorTableHead({ table }: { table: TableType<NetworkMonitorRecord> }) {
	return (
		<TableHeader className="sticky top-0 z-50 w-full border-b-2">
			{table.getHeaderGroups().map((headerGroup) => (
				<tr key={headerGroup.id}>
					{headerGroup.headers.map((header) => {
						return (
							<TableHead className="px-2" key={header.id}>
								{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
							</TableHead>
						)
					})}
				</tr>
			))}
		</TableHeader>
	)
}

const NetworkMonitorTableRow = memo(function NetworkMonitorTableRow({
	row,
	isSelected,
	rowSelection: _rowSelection,
	// Column visibility doesn't change the row object identity, so this prop exists only
	// to force a re-render (and a fresh row.getVisibleCells() read) when columns are toggled.
	visibleColumnsKey: _visibleColumnsKey,
	openSheet,
}: {
	row: Row<NetworkMonitorRecord>
	isSelected: boolean
	// Menus depend on the entire selection, including changes to other rows.
	rowSelection: RowSelectionState
	visibleColumnsKey: string
	openSheet: (monitor: NetworkMonitorRecord) => void
}) {
	const system = useStore($allSystemsById)[row.original.system]
	return (
		<TableRow
			data-state={isSelected && "selected"}
			className={cn("cursor-pointer transition-opacity", {
				"opacity-50": system?.status === SystemStatus.Paused,
			})}
			onClick={() => openSheet(row.original)}
		>
			{row.getVisibleCells().map((cell) => (
				<TableCell
					key={cell.id}
					className="py-0"
					style={{
						width: `${cell.column.getSize()}px`,
						height: 54,
					}}
				>
					{flexRender(cell.column.columnDef.cell, cell.getContext())}
				</TableCell>
			))}
		</TableRow>
	)
})
