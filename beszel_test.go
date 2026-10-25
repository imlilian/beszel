package beszel

import (
	"testing"

	"github.com/blang/semver"
)

// TestVersionKeepsUpstreamPrecedence pins the fork's versioning scheme: build
// metadata identifies the fork build, but semver precedence must stay equal to
// the upstream base, because the agent<->hub handshake and the update check
// compare versions while ignoring metadata.
func TestVersionKeepsUpstreamPrecedence(t *testing.T) {
	v, err := semver.Parse(Version)
	if err != nil {
		t.Fatalf("Version %q is not valid semver: %v", Version, err)
	}
	if len(v.Build) == 0 || v.Build[0] != "beszelx" {
		t.Fatalf("Version %q should carry fork build metadata (+beszelx.N)", Version)
	}
	if v.Pre != nil {
		t.Fatalf("Version %q should be a plain release plus metadata, not a pre-release", Version)
	}
	// Precedence is the upstream base, so an upstream patch release still wins.
	if !v.LT(semver.MustParse("0.21.2")) {
		t.Fatalf("Version %q must sort below the next upstream patch release", Version)
	}
}
