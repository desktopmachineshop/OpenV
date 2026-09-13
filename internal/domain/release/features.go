package release

import "sort"

// Feature gating by channel (REQ-137).
//
// The shared service runs one build. A new feature ships in a release and
// is declared here with that release's version; a workspace on the nightly
// channel sees it at once, and a workspace on the stable channel sees it
// once the stable release it has turned on is that release or a later one.
// Maintenance updates and bug fixes are never gated: only what is filed
// under New features waits for a stable release.
//
// A feature stays in the registry until every stable release that could
// lack it has left the support window, then its gate is removed and the
// code path becomes unconditional.

// Feature is one gated change.
type Feature struct {
	// Key names the feature to code and clients ("crew-graph-v2").
	Key string
	// ShippedIn is the version of the release that first carried it.
	ShippedIn string
	// Summary is one line for the features endpoint and the What's new page.
	Summary string
}

// Registry lists every gated feature. Keep it sorted by ShippedIn so the
// oldest gates are the easiest to find and retire.
var Registry = []Feature{
	// No gated feature has shipped yet. The next new feature adds itself
	// here with the release it ships in, for example:
	//   {Key: "example", ShippedIn: "0.3.0", Summary: "…"},
}

// Enabled reports whether a feature is on for a workspace on the given
// channel whose turned-on stable release is stableRelease ("" when the
// workspace has no stable release yet).
func Enabled(f Feature, channel, stableRelease string) bool {
	if channel != "stable" {
		return true
	}
	if stableRelease == "" {
		return false
	}
	return AtOrBefore(f.ShippedIn, stableRelease)
}

// FeaturesFor resolves every registered feature for a workspace: its
// channel and the stable release it has turned on. A version the notes do
// not know as a stable release leaves every gate closed on the stable
// channel, so a mistyped or withdrawn release never opens anything.
func FeaturesFor(svc Service, channel, stableVersion string) map[string]bool {
	stable := stableVersion
	if svc != nil && stableVersion != "" && svc.Stable(stableVersion) == nil {
		stable = ""
	}
	out := make(map[string]bool, len(Registry))
	for _, f := range Registry {
		out[f.Key] = Enabled(f, channel, stable)
	}
	return out
}

// Keys lists the registered feature keys, sorted.
func Keys() []string {
	keys := make([]string, 0, len(Registry))
	for _, f := range Registry {
		keys = append(keys, f.Key)
	}
	sort.Strings(keys)
	return keys
}
