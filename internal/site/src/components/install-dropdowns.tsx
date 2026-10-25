import { memo } from "react"
import { copyToClipboard, getHubURL } from "@/lib/utils"
import { DropdownMenuContent, DropdownMenuItem } from "./ui/dropdown-menu"

/**
 * Install commands shown in the "add system" dialog.
 *
 * This tree is a fork and publishes its own agent image and install scripts, so
 * every command below points at the fork, never at upstream. Keep these in sync
 * with what `.github/workflows/docker-images.yml` and `release.yml` publish:
 *
 *   image   → ghcr.io/<repo>/beszel-agent   (built on every `v*` tag, must be a
 *                                          public package for pulls to work)
 *   script  → raw.githubusercontent.com/<repo>/main/supplemental/scripts/...
 *
 * The same strings are duplicated inside the shell scripts (`BESZEL_REPO`), so
 * a rename has to touch both places.
 */
const FORK_REPO = "imlilian/beszel"
const FORK_BRANCH = "main"
const AGENT_IMAGE = `ghcr.io/${FORK_REPO}/beszel-agent`
const AGENT_SCRIPT_URL = `https://raw.githubusercontent.com/${FORK_REPO}/${FORK_BRANCH}/supplemental/scripts/install-agent.sh`

export function copyDockerCompose(port = "45876", publicKey: string, token: string) {
	copyToClipboard(`services:
  beszel-agent:
    image: ${AGENT_IMAGE}
    container_name: beszel-agent
    restart: unless-stopped
    network_mode: host
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./beszel_agent_data:/var/lib/beszel-agent
      # monitor other disks / partitions by mounting a folder in /extra-filesystems
      # - /mnt/disk/.beszel:/extra-filesystems/sda1:ro
    environment:
      LISTEN: ${port}
      KEY: '${publicKey}'
      TOKEN: ${token}
      HUB_URL: ${getHubURL()}`)
}

export function copyDockerRun(port = "45876", publicKey: string, token: string) {
	copyToClipboard(
		`docker run -d --name beszel-agent --network host --restart unless-stopped -v /var/run/docker.sock:/var/run/docker.sock:ro -v beszel_agent_data:/var/lib/beszel-agent -e KEY="${publicKey}" -e LISTEN=${port} -e TOKEN="${token}" -e HUB_URL="${getHubURL()}" ${AGENT_IMAGE}`
	)
}

export function copyLinuxCommand(port = "45876", publicKey: string, token: string) {
	// The script resolves the release from this fork (see BESZEL_REPO inside it),
	// so the command only has to fetch and run it. `--mirror=<url>` is still
	// available for networks that cannot reach GitHub, but there is no default
	// mirror any more: upstream's gh.beszel.dev only mirrors upstream releases.
	copyToClipboard(
		`curl -sL ${AGENT_SCRIPT_URL} -o /tmp/install-agent.sh && chmod +x /tmp/install-agent.sh && /tmp/install-agent.sh -p ${port} -k "${publicKey}" -t "${token}" -url "${getHubURL()}"`
	)
}

export interface DropdownItem {
	text: string
	onClick?: () => void
	url?: string
	icons?: React.ComponentType<React.SVGProps<SVGSVGElement>>[]
}

export const InstallDropdown = memo(({ items }: { items: DropdownItem[] }) => {
	return (
		<DropdownMenuContent align="end">
			{items.map((item, index) => {
				const className = "cursor-pointer flex items-center gap-1.5"
				return item.url ? (
					<DropdownMenuItem key={index} asChild>
						<a href={item.url} className={className} target="_blank" rel="noopener noreferrer">
							{item.text}{" "}
							{item.icons?.map((Icon, iconIndex) => (
								<Icon key={iconIndex} className="size-4" />
							))}
						</a>
					</DropdownMenuItem>
				) : (
					<DropdownMenuItem key={index} onClick={item.onClick} className={className}>
						{item.text}{" "}
						{item.icons?.map((Icon, iconIndex) => (
							<Icon key={iconIndex} className="size-4" />
						))}
					</DropdownMenuItem>
				)
			})}
		</DropdownMenuContent>
	)
})
