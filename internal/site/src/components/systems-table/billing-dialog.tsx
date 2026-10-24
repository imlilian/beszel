import { Trans, useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { WalletIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { toast } from "@/components/ui/use-toast"
import { isReadOnlyUser, pb } from "@/lib/api"
import {
	$billingBySystemId,
	$trafficBySystemId,
	type BillingRecord,
	type BillingType,
	type Currency,
	type Cycle,
	type TrafficMode,
	type TrafficRecord,
} from "@/lib/billing"
import {
	addCycle,
	formatPrice,
	fromDateInputValue,
	getBillingExpiryState,
	getExpirySeverity,
	getTrafficState,
	parsePbDate,
	toDateInputValue,
} from "@/lib/billing-utils"
import { $userSettings } from "@/lib/stores"
import { cn, decimalString, formatBytes } from "@/lib/utils"
import type { SystemRecord } from "@/types"

/* ------------------------------------------------------------------ */
/* Localized labels                                                    */
/* ------------------------------------------------------------------ */

function expirySeverityClass(sev: "ok" | "warn" | "crit"): string {
	return sev === "crit" ? "text-red-500" : sev === "warn" ? "text-yellow-500" : "text-muted-foreground"
}

/** Localized short cycle suffix (`/mo`, `/yr`, ...). Shared by the summary and the dialog preview. */
function CycleSuffix({ cycle }: { cycle: Cycle }) {
	const { t } = useLingui()
	switch (cycle) {
		case "monthly":
			return <>{t`/mo`}</>
		case "semiannual":
			return <>{t`/6mo`}</>
		case "annual":
			return <>{t`/yr`}</>
		case "biennial":
			return <>{t`/2yr`}</>
		case "triennial":
			return <>{t`/3yr`}</>
		case "permanent":
			return null
		default:
			return null
	}
}

/* ------------------------------------------------------------------ */
/* Card summary + traffic bar                                          */
/* ------------------------------------------------------------------ */

/**
 * Compact billing line shown under a system card and in the table column.
 * Renders nothing without a billing record, so both callers can mount it
 * unconditionally and let it decide (and subscribe) on its own.
 *
 * `bordered` keeps the card's top divider; the table cell passes `false` to
 * avoid a redundant line on top of the cell separator.
 */
export function BillingSummary({ systemId, bordered = true }: { systemId: string; bordered?: boolean }) {
	const { t } = useLingui()
	const billing = useStore($billingBySystemId)[systemId]
	const traffic = useStore($trafficBySystemId)[systemId]
	if (!billing) return null

	const state = getBillingExpiryState(billing.billingType, billing.cycle, parsePbDate(billing.expiresAt))
	let expiryText: string | null = null
	let severityClass = "text-muted-foreground"
	if (state.kind === "permanent") {
		expiryText = t`Permanent`
	} else if (state.kind === "active") {
		const daysLeft = state.daysLeft
		if (daysLeft === 0) {
			expiryText = t`Expires today`
			severityClass = "text-red-500"
		} else {
			expiryText = t`${daysLeft} days left`
			severityClass = expirySeverityClass(getExpirySeverity(daysLeft))
		}
	} else if (state.kind === "expired") {
		const daysAgo = Math.abs(state.daysLeft)
		expiryText = t`expired ${daysAgo} days ago`
		severityClass = "text-red-500"
	}

	return (
		<div className={cn("flex flex-col gap-1.5 text-xs", bordered && "border-t pt-2")}>
			<div className="flex items-center gap-1.5">
				<WalletIcon className="size-3.5 text-muted-foreground" />
				<span className={severityClass}>
					{billing.billingType === "free" ? (
						t({ id: "billing-free", message: "Free" })
					) : (
						<>
							{formatPrice(billing.price, billing.currency)}
							<CycleSuffix cycle={billing.cycle} />
						</>
					)}
				</span>
				{expiryText && (
					<>
						<span className="text-muted-foreground">·</span>
						<span className={severityClass}>{expiryText}</span>
					</>
				)}
			</div>
			{traffic && billing.trafficLimit > 0 && <TrafficBar billing={billing} traffic={traffic} />}
		</div>
	)
}

function TrafficBar({ billing, traffic }: { billing: BillingRecord; traffic: TrafficRecord }) {
	const { t } = useLingui()
	const unitNet = useStore($userSettings, { keys: ["unitNet"] }).unitNet
	const used = billing.trafficMode === "upload" ? traffic.usedUpload : traffic.usedUpload + traffic.usedDownload
	const state = getTrafficState(billing.trafficLimit, used, parsePbDate(traffic.periodEnd))

	const fmt = (bytes: number) => {
		const { value, unit } = formatBytes(bytes, false, unitNet)
		return `${decimalString(value, value >= 100 ? 1 : 2)}${unit}`
	}
	const usedStr = fmt(used)
	const limitStr = fmt(billing.trafficLimit)
	const pct = Math.round(state.pct)

	const barColor = state.exceeded ? "bg-red-500" : pct >= 80 ? "bg-yellow-500" : "bg-green-500"
	const daysLeft = state.daysLeft

	return (
		<div className="flex flex-col gap-1">
			<div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
				<div className={cn("h-full", barColor)} style={{ width: `${pct}%` }} />
			</div>
			<div className="text-muted-foreground">
				{t`Used ${usedStr} / ${limitStr}`}
				{" · "}
				{t`resets in ${daysLeft} days`}
			</div>
		</div>
	)
}

/* ------------------------------------------------------------------ */
/* Dialog                                                              */
/* ------------------------------------------------------------------ */

const GB = 1024 ** 3
const TB = 1024 ** 4
type TrafficUnit = "GB" | "TB"

interface BillingForm {
	system: string
	billingType: BillingType
	price: string
	currency: Currency
	cycle: Cycle
	paidAt: string
	expiresAt: string
	notifyDaysBefore: string
	trafficLimit: string
	trafficUnit: TrafficUnit
	trafficMode: TrafficMode
	resetDay: string
	trafficAlerts: boolean
}

function pickUnit(bytes: number): TrafficUnit {
	return bytes >= TB && bytes % TB === 0 ? "TB" : "GB"
}

function initialForm(rec?: BillingRecord): BillingForm {
	return {
		system: rec?.system ?? "",
		billingType: rec?.billingType ?? "paid",
		price: rec?.price != null ? String(rec.price) : "",
		currency: rec?.currency ?? "CNY",
		cycle: rec?.cycle ?? "monthly",
		paidAt: rec?.paidAt ? toDateInputValue(rec.paidAt) : "",
		expiresAt: rec?.expiresAt ? toDateInputValue(rec.expiresAt) : "",
		notifyDaysBefore: rec?.notifyDaysBefore != null ? String(rec.notifyDaysBefore) : "7",
		trafficLimit: rec?.trafficLimit ? String(rec.trafficLimit) : "",
		trafficUnit: pickUnit(rec?.trafficLimit ?? 0),
		trafficMode: rec?.trafficMode ?? "sum",
		resetDay: rec?.resetDay ? String(rec.resetDay) : "1",
		// §4.1 #4: default true; the backend hook cannot distinguish "unset" from explicit false.
		trafficAlerts: rec?.trafficAlerts ?? true,
	}
}

function toBytes(display: string, unit: TrafficUnit): number {
	const n = display === "" ? 0 : Number(display)
	return Math.round(n * (unit === "TB" ? TB : GB))
}

export function BillingDialog({
	system,
	open,
	onOpenChange,
}: {
	system: SystemRecord
	open: boolean
	onOpenChange: (open: boolean) => void
}) {
	const { t } = useLingui()
	const readOnly = isReadOnlyUser()
	const [form, setForm] = useState<BillingForm>(() => initialForm($billingBySystemId.get()[system.id]))

	// Re-seed the form from the latest store value each time the dialog opens.
	useEffect(() => {
		if (open) setForm(initialForm($billingBySystemId.get()[system.id]))
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [open, system.id])

	const set = <K extends keyof BillingForm>(key: K, value: BillingForm[K]) => setForm((f) => ({ ...f, [key]: value }))

	const isFree = form.billingType === "free"
	const isPermanent = form.cycle === "permanent"
	const expiresRequired = !isFree && !isPermanent
	const systemName = system.name

	const estimateExpiry = () => {
		if (!form.paidAt || isPermanent) return
		const base = fromDateInputValue(form.paidAt)
		if (!base) return
		const next = addCycle(base, form.cycle)
		if (next.getTime() === 0) return
		set("expiresAt", next.toISOString().slice(0, 10))
	}

	const handleUnitChange = (unit: TrafficUnit) => {
		if (unit === form.trafficUnit) return
		// Keep the byte value stable while switching the displayed unit; trim the
		// long float tail so the input doesn't show e.g. 0.0009765625.
		const bytes = toBytes(form.trafficLimit, form.trafficUnit)
		const factor = unit === "TB" ? TB : GB
		set("trafficUnit", unit)
		set("trafficLimit", String(Number((bytes / factor).toFixed(6))))
	}

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault()
		if (expiresRequired && !form.expiresAt) {
			toast({
				title: t`Save failed`,
				description: t`Expires at is required for a paid system that is not permanent.`,
				variant: "destructive",
			})
			return
		}
		if (expiresRequired && form.paidAt && form.expiresAt && form.expiresAt <= form.paidAt) {
			toast({
				title: t`Save failed`,
				description: t`Expiry date must be after the paid date.`,
				variant: "destructive",
			})
			return
		}
		const payload = {
			system: system.id,
			billingType: form.billingType,
			price: isFree ? 0 : form.price === "" ? 0 : Number(form.price),
			currency: form.currency,
			cycle: form.cycle,
			paidAt: fromDateInputValue(form.paidAt) ?? null,
			expiresAt: expiresRequired ? fromDateInputValue(form.expiresAt) ?? null : null,
			notifyDaysBefore: form.notifyDaysBefore === "" ? 7 : Number(form.notifyDaysBefore),
			trafficLimit: toBytes(form.trafficLimit, form.trafficUnit),
			trafficMode: form.trafficMode,
			resetDay: form.resetDay === "" ? 1 : Number(form.resetDay),
			trafficAlerts: form.trafficAlerts,
		}
		try {
			const existing = $billingBySystemId.get()[system.id]
			let record: BillingRecord
			if (existing?.id) {
				record = await pb.collection<BillingRecord>("system_billing").update(existing.id, payload)
			} else {
				record = await pb.collection<BillingRecord>("system_billing").create(payload)
			}
			// optimistic update so the card refreshes without waiting for the subscription
			$billingBySystemId.set({ ...$billingBySystemId.get(), [system.id]: record })
			toast({ description: t`Settings saved` })
			onOpenChange(false)
		} catch (err: any) {
			// Surface the backend's validation message verbatim (e.g. the 400 about a missing expiresAt).
			const msg =
				err?.data?.message || err?.message || t`Failed to save billing settings. Please try again.`
			toast({ title: t`Save failed`, description: msg, variant: "destructive" })
		}
	}

	// Live preview line for the cost tab.
	const preview = (() => {
		if (isFree) return t({ id: "billing-free-no-expiry", message: "Free — no expiry tracked" })
		const pricePart = (
			<>
				{formatPrice(form.price === "" ? 0 : Number(form.price), form.currency)}
				<CycleSuffix cycle={form.cycle} />
			</>
		)
		const state = getBillingExpiryState(
			form.billingType,
			form.cycle,
			parsePbDate(form.expiresAt ? `${form.expiresAt}T00:00:00.000Z` : "")
		)
		const datePart = form.expiresAt ? <> ({form.expiresAt})</> : null
		if (state.kind === "permanent") {
			return (
				<>
					{pricePart} · {t`Permanent`}
				</>
			)
		}
		if (state.kind === "active") {
			const daysLeft = state.daysLeft
			const daysText = daysLeft === 0 ? t`Expires today` : t`${daysLeft} days left`
			return (
				<>
					{pricePart} · {daysText}
					{datePart}
				</>
			)
		}
		if (state.kind === "expired") {
			const daysAgo = Math.abs(state.daysLeft)
			return (
				<>
					{pricePart} · {t`expired ${daysAgo} days ago`}
					{datePart}
				</>
			)
		}
		return <>{pricePart}</>
	})()

	const fieldDisabled = readOnly

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-lg">
				<DialogHeader>
					<DialogTitle>
						<Trans>Billing & Traffic</Trans>
					</DialogTitle>
					<DialogDescription>{t`Cost and traffic quota for ${systemName}.`}</DialogDescription>
				</DialogHeader>

				<form onSubmit={handleSubmit} className="grid gap-4">
					<Tabs defaultValue="cost" className="w-full">
						<TabsList className="grid w-full grid-cols-2">
							<TabsTrigger value="cost">
								<Trans>Cost</Trans>
							</TabsTrigger>
							<TabsTrigger value="traffic">
								<Trans>Traffic</Trans>
							</TabsTrigger>
						</TabsList>

						{/* ---------------- Cost ---------------- */}
						<TabsContent value="cost" className="mt-3 grid gap-3">
							<div className="grid gap-2">
								<Label htmlFor="billingType">
									<Trans>Billing type</Trans>
								</Label>
								<Select
									value={form.billingType}
									onValueChange={(v) => set("billingType", v as BillingType)}
									disabled={fieldDisabled}
								>
									<SelectTrigger id="billingType">
										<SelectValue />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="paid">
											<Trans>Paid</Trans>
										</SelectItem>
										<SelectItem value="free">
											<Trans id="billing-type-free">Free</Trans>
										</SelectItem>
									</SelectContent>
								</Select>
							</div>

							{!isFree && (
								<>
									<div className="grid grid-cols-2 gap-3">
										<div className="grid gap-2">
											<Label htmlFor="price">
												<Trans>Price</Trans>
											</Label>
											<Input
												id="price"
												type="number"
												min={0}
												step="0.01"
												value={form.price}
												disabled={fieldDisabled}
												onChange={(e) => set("price", e.target.value)}
											/>
										</div>
										<div className="grid gap-2">
											<Label htmlFor="currency">
												<Trans>Currency</Trans>
											</Label>
											<Select
												value={form.currency}
												onValueChange={(v) => set("currency", v as Currency)}
												disabled={fieldDisabled}
											>
												<SelectTrigger id="currency">
													<SelectValue />
												</SelectTrigger>
												<SelectContent>
													<SelectItem value="CNY">CNY</SelectItem>
													<SelectItem value="USD">USD</SelectItem>
													<SelectItem value="EUR">EUR</SelectItem>
													<SelectItem value="GBP">GBP</SelectItem>
												</SelectContent>
											</Select>
										</div>
									</div>

									<div className="grid gap-2">
										<Label htmlFor="cycle">
											<Trans>Billing cycle</Trans>
										</Label>
										<Select
											value={form.cycle}
											onValueChange={(v) => set("cycle", v as Cycle)}
											disabled={fieldDisabled}
										>
											<SelectTrigger id="cycle">
												<SelectValue />
											</SelectTrigger>
											<SelectContent>
												<SelectItem value="monthly">
													<Trans>Monthly</Trans>
												</SelectItem>
												<SelectItem value="semiannual">
													<Trans>Semi-annual</Trans>
												</SelectItem>
												<SelectItem value="annual">
													<Trans>Annual</Trans>
												</SelectItem>
												<SelectItem value="biennial">
													<Trans>Biennial</Trans>
												</SelectItem>
												<SelectItem value="triennial">
													<Trans>Triennial</Trans>
												</SelectItem>
												<SelectItem value="permanent">
													<Trans>Permanent</Trans>
												</SelectItem>
											</SelectContent>
										</Select>
									</div>

									<div className="grid gap-2">
										<Label htmlFor="paidAt">
											<Trans>Paid date</Trans>
										</Label>
										<Input
											id="paidAt"
											type="date"
											value={form.paidAt}
											disabled={fieldDisabled}
											onChange={(e) => set("paidAt", e.target.value)}
										/>
									</div>

									{!isPermanent && (
										<div className="grid gap-2">
											<Label htmlFor="expiresAt">
												<Trans>Expiry date</Trans>
											</Label>
											<div className="flex gap-2">
												<Input
													id="expiresAt"
													type="date"
													value={form.expiresAt}
													disabled={fieldDisabled}
													onChange={(e) => set("expiresAt", e.target.value)}
													className="flex-1"
												/>
												<Button
													type="button"
													variant="outline"
													disabled={fieldDisabled || !form.paidAt}
													onClick={estimateExpiry}
												>
													<Trans>Estimate from cycle</Trans>
												</Button>
											</div>
										</div>
									)}

									<div className="grid gap-2">
										<Label htmlFor="notifyDaysBefore">
											<Trans>Notify</Trans>
										</Label>
										<div className="flex items-center gap-2">
											<Input
												id="notifyDaysBefore"
												type="number"
												min={1}
												max={60}
												value={form.notifyDaysBefore}
												disabled={fieldDisabled}
												onChange={(e) => set("notifyDaysBefore", e.target.value)}
												className="w-24"
											/>
											<span className="text-sm text-muted-foreground">
												<Trans>days before expiry</Trans>
											</span>
										</div>
									</div>
								</>
							)}

							<div className="rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">{preview}</div>
						</TabsContent>

						{/* ---------------- Traffic ---------------- */}
						<TabsContent value="traffic" className="mt-3 grid gap-3">
							<div className="grid grid-cols-[1fr_auto] gap-3">
								<div className="grid gap-2">
									<Label htmlFor="trafficLimit">
										<Trans>Traffic limit</Trans>
									</Label>
									<Input
										id="trafficLimit"
										type="number"
										min={0}
										value={form.trafficLimit}
										disabled={fieldDisabled}
										onChange={(e) => set("trafficLimit", e.target.value)}
										placeholder="0"
									/>
								</div>
								<div className="grid gap-2">
									<Label htmlFor="trafficUnit">
										<Trans>Unit</Trans>
									</Label>
									<Select
										value={form.trafficUnit}
										onValueChange={(v) => handleUnitChange(v as TrafficUnit)}
										disabled={fieldDisabled}
									>
										<SelectTrigger id="trafficUnit" className="w-24">
											<SelectValue />
										</SelectTrigger>
										<SelectContent>
											<SelectItem value="GB">GB</SelectItem>
											<SelectItem value="TB">TB</SelectItem>
										</SelectContent>
									</Select>
								</div>
							</div>
							<p className="text-xs text-muted-foreground">
								<Trans>Set to 0 for unlimited traffic (no card bar).</Trans>
							</p>

							<div className="grid gap-2">
								<Label htmlFor="trafficMode">
									<Trans>Traffic mode</Trans>
								</Label>
								<Select
									value={form.trafficMode}
									onValueChange={(v) => set("trafficMode", v as TrafficMode)}
									disabled={fieldDisabled}
								>
									<SelectTrigger id="trafficMode">
										<SelectValue />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="sum">
											<Trans>Upload + download</Trans>
										</SelectItem>
										<SelectItem value="upload">
											<Trans>Upload only</Trans>
										</SelectItem>
									</SelectContent>
								</Select>
							</div>

							<div className="grid gap-2">
								<Label htmlFor="resetDay">
									<Trans>Reset day</Trans>
								</Label>
								<Input
									id="resetDay"
									type="number"
									min={1}
									max={31}
									value={form.resetDay}
									disabled={fieldDisabled}
									onChange={(e) => set("resetDay", e.target.value)}
									className="w-24"
								/>
							</div>

							<div className="flex items-center justify-between rounded-md border p-3">
								<div className="grid gap-1">
									<Label htmlFor="trafficAlerts">
										<Trans>Traffic alerts</Trans>
									</Label>
									<p className="text-xs text-muted-foreground">
										<Trans>Warn when usage passes the threshold.</Trans>
									</p>
								</div>
								<Switch
									id="trafficAlerts"
									checked={form.trafficAlerts}
									onCheckedChange={(v) => set("trafficAlerts", v)}
									disabled={fieldDisabled}
								/>
							</div>
						</TabsContent>
					</Tabs>

					{!readOnly && (
						<DialogFooter>
							<Button type="submit">
								<Trans>Save</Trans>
							</Button>
						</DialogFooter>
					)}
				</form>
			</DialogContent>
		</Dialog>
	)
}
