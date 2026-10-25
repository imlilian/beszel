package beszel

import (
	"strings"
	"testing"

	"github.com/blang/semver"
)

// TestVersionIdentifiesTheFork pins the fork's versioning scheme:
//
//   - it must read `<upstream base>-beszelx.<n>`, with the base equal to the
//     upstream release the tree is based on (a merge updates only the base);
//   - it must not use build metadata (`+`): the same string is the git tag and
//     becomes a Docker tag, where `+` is illegal;
//   - every MinVersion* constant must stay below it, otherwise the agent
//     handshake would silently degrade to the legacy path.
func TestVersionIdentifiesTheFork(t *testing.T) {
	v, err := semver.Parse(Version)
	if err != nil {
		t.Fatalf("Version %q is not valid semver: %v", Version, err)
	}
	if strings.Contains(Version, "+") {
		t.Fatalf("Version %q must not carry build metadata: it is reused as a Docker tag", Version)
	}
	if len(v.Pre) != 2 || v.Pre[0].String() != "beszelx" {
		t.Fatalf("Version %q should end in -beszelx.<n>", Version)
	}
	if v.Major != 0 || v.Minor != 21 || v.Patch != 1 {
		t.Fatalf("Version %q should be based on the upstream release it forked from (currently 0.21.1)", Version)
	}
	for name, min := range map[string]semver.Version{
		"MinVersionCbor":            MinVersionCbor,
		"MinVersionAgentResponse":   MinVersionAgentResponse,
		"MinVersionZfsData":         MinVersionZfsData,
		"MinVersionNetworkMonitors": MinVersionNetworkMonitors,
	} {
		if !v.GT(min) {
			t.Fatalf("Version %q must sort above %s (%s)", Version, name, min)
		}
	}
}
