// Package beszel provides core application constants and version information
// which are used throughout the application.
package beszel

import "github.com/blang/semver"

const (
	// Version is the current version of the application.
	//
	// This tree is a fork that keeps merging upstream releases, so the version
	// is "upstream base + build metadata": the part before `+` must stay equal
	// to the upstream release it is based on (that is what a merge updates),
	// and `beszelx.N` counts the fork's own feature rounds on top of it.
	//
	// Build metadata is ignored by semver precedence, so every MinVersion*
	// comparison, the agent<->hub handshake and the update check behave exactly
	// as they do for the upstream release. Do not switch to a pre-release
	// suffix (`0.22.0-beszelx.1`): it would sort above the base and make the
	// next upstream patch release look like a downgrade.
	Version = "0.21.1+beszelx.1"
	// AppName is the name of the application.
	AppName = "beszel"
)

// MinVersionCbor is the minimum supported version for CBOR compatibility.
var MinVersionCbor = semver.MustParse("0.12.0")

// MinVersionAgentResponse is the minimum supported version for AgentResponse compatibility.
var MinVersionAgentResponse = semver.MustParse("0.13.0")

// MinVersionZfsData is the minimum agent version that supports ZFS detail requests.
var MinVersionZfsData = semver.MustParse("0.18.9")

// MinVersionNetworkMonitors is the minimum agent version that supports network monitor sync.
var MinVersionNetworkMonitors = semver.MustParse("0.20.0")
