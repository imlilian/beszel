// Package beszel provides core application constants and version information
// which are used throughout the application.
package beszel

import "github.com/blang/semver"

const (
	// Version is the current version of the application.
	//
	// This tree is a fork that keeps merging upstream releases, so the version
	// reads "upstream base + fork round": the part before `-beszelx` must stay
	// equal to the upstream release it is based on (a merge updates it), and
	// `beszelx.N` counts the fork's own feature rounds on top of it.
	//
	// The `-` form is deliberate. Semver pre-release identifiers are also legal
	// Docker tags, while build metadata (`0.21.1+beszelx.1`) is not, and the git
	// tag has to be the same string as Version so that released artifacts,
	// `ghupdate` and the install scripts all agree. Keep the tag as
	// `v<Version>`.
	Version = "0.21.1-beszelx.1"
	// AppName is the name of the application.
	AppName = "beszel"

	// RepoOwner and RepoName point at this fork's repository. Self-update, the
	// update check and the release links use them instead of upstream, so a
	// fork never installs an upstream binary over itself.
	RepoOwner = "imlilian"
	RepoName  = "beszel"
)

// MinVersionCbor is the minimum supported version for CBOR compatibility.
var MinVersionCbor = semver.MustParse("0.12.0")

// MinVersionAgentResponse is the minimum supported version for AgentResponse compatibility.
var MinVersionAgentResponse = semver.MustParse("0.13.0")

// MinVersionZfsData is the minimum agent version that supports ZFS detail requests.
var MinVersionZfsData = semver.MustParse("0.18.9")

// MinVersionNetworkMonitors is the minimum agent version that supports network monitor sync.
var MinVersionNetworkMonitors = semver.MustParse("0.20.0")
