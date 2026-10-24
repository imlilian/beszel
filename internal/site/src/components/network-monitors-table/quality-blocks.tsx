import { memo } from "react"
import { useLingui } from "@lingui/react/macro"
import { hasPartialLoss, type QualityBlock } from "@/lib/monitor-blocks"
import { monitorLevelBg, type MonitorLevel } from "@/lib/monitor-thresholds"
import { cn, formatMicroseconds, hourWithMinutes } from "@/lib/utils"

/** Levels that carry counts in the screen-reader summary, in reading order. */
const SUMMARY_LEVELS: MonitorLevel[] = ["good", "warn", "crit", "loss", "empty"]

function timeRange(block: QualityBlock) {
	return `${hourWithMinutes(new Date(block.startMs).toISOString())}–${hourWithMinutes(new Date(block.endMs).toISOString())}`
}

/**
 * The 20 vertical quality blocks for one monitor.
 *
 * `blocks` arrives newest-first (index 0 is the current block); they are reversed here so
 * the newest block sits at the inline end of the strip. A block's main color is the latency
 * level, and partial packet loss adds a red bottom edge instead of turning the whole block red.
 */
export const QualityBlocks = memo(function QualityBlocks({
	blocks,
	className,
}: {
	blocks: QualityBlock[]
	className?: string
}) {
	const { t } = useLingui()
	// Oldest first, so the strip reads left to right (or right to left in RTL).
	const ordered = blocks.slice().reverse()

	const summary = SUMMARY_LEVELS.map((level) => ({
		level,
		count: ordered.filter((block) => block.level === level).length,
	}))
		.filter(({ count }) => count > 0)
		.map(({ level, count }) => {
			switch (level) {
				case "good":
					return t`${count} good`
				case "warn":
					return t`${count} warning`
				case "crit":
					return t`${count} critical`
				case "loss":
					return t`${count} lost`
				default:
					return t`${count} no data`
			}
		})
		.join(", ")
	const ariaLabel = t`Latency quality, oldest to newest: ${summary}`

	return (
		<div
			className={cn("flex h-3 items-stretch gap-[2px] rtl:flex-row-reverse", className)}
			role="img"
			aria-label={ariaLabel}
		>
			{ordered.map((block, index) => {
				const parts = [timeRange(block)]
				if (block.total === 0) {
					parts.push(t`No probes`)
				} else {
					if (block.avgUs !== null) {
						parts.push(`${t`Avg`} ${formatMicroseconds(block.avgUs)}`)
					}
					if (block.minUs !== null && block.maxUs !== null) {
						parts.push(
							`${t`Range`} ${formatMicroseconds(block.minUs, false)}–${formatMicroseconds(block.maxUs, false)}`
						)
					}
					parts.push(`${t`Loss`} ${block.total - block.success}/${block.total} (${block.lossPct.toFixed(0)}%)`)
					parts.push(t`${block.total} probes`)
				}
				return (
					<div
						// Blocks are positional, so the index is the stable identity here.
						key={index}
						title={parts.join(" · ")}
						className={cn(
							"flex-1 rounded-[1px]",
							monitorLevelBg[block.level],
							hasPartialLoss(block) && "border-b-2 border-red-700"
						)}
					/>
				)
			})}
		</div>
	)
})
