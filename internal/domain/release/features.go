package release

import "sort"

// Feature gating by channel (REQ-137).
//
// The shared service runs one build. A user-visible change ships in a
// nightly and is declared here with that nightly's version; a workspace on
// the nightly channel sees it at once, and a workspace on the stable channel
// sees it once the stable release it has turned on was cut from that
// nightly or a later one. Fixes are never gated: they are not features.
//
// A feature stays in the registry until every stable release that could
// lack it has left the support window, then its gate is removed and the
// code path becomes unconditional.

// Feature is one gated change.
type Feature struct {
	// Key names the feature to code and clients ("crew-graph-v2").
	Key string
	// ShippedIn is the nightly version that first carried it.
	ShippedIn string
	// Summary is one line for the features endpoint and the What's new page.
	Summary string
}

// Registry lists every gated feature. Keep it sorted by ShippedIn so the
// oldest gates are the easiest to find and retire.
var Registry = []Feature{
	// No gated feature has shipped yet. The next user-visible change adds
	// itself here with the nightly it ships in, for example:
	//   {Key: "example", ShippedIn: "2026-09-20", Summary: "…"},
}

// Enabled reports whether a feature is on for a workspace on the given
// channel whose turned-on stable release was cut from stableCutFrom (""
// when the workspace has no stable release yet).
func Enabled(f Feature, channel, stableCutFrom string) bool {
	if channel != "stable" {
		return true
	}
	if stableCutFrom == "" {
		return false
	}
	return NightlyAtOrBefore(f.ShippedIn, stableCutFrom)
}

// FeaturesFor resolves every registered feature for a workspace: its
// channel and the stable release it has turned on (looked up in the notes
// for the nightly it was cut from). Unknown or empty stable versions leave
// every gate closed on the stable channel.
func FeaturesFor(svc Service, channel, stableVersion string) map[string]bool {
	cutFrom := ""
	if svc != nil && stableVersion != "" {
		if s := svc.Stable(stableVersion); s != nil {
			cutFrom = s.CutFrom
		}
	}
	out := make(map[string]bool, len(Registry))
	for _, f := range Registry {
		out[f.Key] = Enabled(f, channel, cutFrom)
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
