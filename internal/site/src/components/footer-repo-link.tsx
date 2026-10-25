import { GithubIcon } from "lucide-react"
import { Separator } from "./ui/separator"

/**
 * Footer links, pointed at this fork's repository rather than upstream.
 *
 * The "new version available" badge that upstream renders here is deliberately
 * absent. This build is a fork whose version carries its own `-beszelx.N`
 * suffix, so an in-app update prompt can only do harm: it either advertises an
 * upstream release that lacks every extension in this tree, or duplicates the
 * `beszel update` CLI command, which already targets this fork's releases.
 * Upgrading is therefore a deliberate act (pull + rebuild, or pull the image),
 * never a click in the UI.
 *
 * The update check itself (`/api/beszel/update`, opt-in via CHECK_UPDATES) is
 * still wired to this fork's releases and keeps filling `$newVersion`, so the
 * information is a one-line render away if it is ever wanted again.
 */
export function FooterRepoLink() {
	return (
		<div className="flex gap-1.5 justify-end items-center pe-3 sm:pe-6 mt-3.5 mb-4 text-xs opacity-80">
			<a
				href="https://github.com/imlilian/beszel"
				target="_blank"
				className="flex items-center gap-0.5 text-muted-foreground hover:text-foreground duration-75"
				rel="noopener"
			>
				<GithubIcon className="h-3 w-3" /> GitHub
			</a>
			<Separator orientation="vertical" className="h-2.5 bg-muted-foreground opacity-70" />
			<a
				href="https://github.com/imlilian/beszel/releases"
				target="_blank"
				className="text-muted-foreground hover:text-foreground duration-75"
				rel="noopener"
			>
				Beszel {globalThis.BESZEL.HUB_VERSION}
			</a>
		</div>
	)
}
