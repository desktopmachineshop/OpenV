package contract

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/release"
)

// phoneAudit is the layout audit's script (e2e/README.md). It renders every
// screen against a mocked API, whose answer to a workspace's feature gates
// turns gated screens and controls on by key.
const phoneAudit = "e2e/tools/phone-audit.js"

// TestPhoneAuditFeatureKeys holds the feature keys e2e/tools/phone-audit.js
// turns on for its mocked workspace to release.Registry (refactor plan X4b).
// The frontend's keys are typed by the generated contract, so tsc refuses
// one Go does not register; the audit is plain JavaScript outside that
// check, and a key Go renames or drops would leave its screens gated off
// there unnoticed, since the audit runs by hand and not in CI.
func TestPhoneAuditFeatureKeys(t *testing.T) {
	src := loadSources(t)
	data, err := os.ReadFile(src.path(phoneAudit))
	if err != nil {
		t.Fatal(err)
	}
	keys := phoneAuditFeatureKeys(t, string(data))
	registered := map[string]bool{}
	for _, f := range release.Registry {
		registered[f.Key] = true
	}
	for _, k := range keys {
		if !registered[k] {
			t.Errorf("%s turns on the feature %q, which release.Registry (internal/domain/release/features.go) "+
				"does not register: use the key Go has, as frontend/src/generated/contract.ts lists it in "+
				"FEATURE_KEYS", phoneAudit, k)
		}
	}
}

// The mocked route for GET /api/v1/orgs/{id}/features, as the audit's
// answer table writes its regular expression, and the features object of
// the answer beside it.
var (
	phoneAuditFeaturesRoute = regexp.MustCompile(`\[/\\/api\\/v1\\/orgs\\/\[\^/\]\+\\/features/,`)
	phoneAuditFeaturesMap   = regexp.MustCompile(`\bfeatures:\s*\{([^{}]*)\}`)
	phoneAuditFeatureEntry  = regexp.MustCompile(`^\s*(?:'([^'\\]*)'|"([^"\\]*)"|([A-Za-z_$][\w$]*))\s*:\s*(true|false)\s*$`)
)

// phoneAuditFeatureKeys reads the keys of the features object in the
// audit's one mocked answer for a workspace's gates. It fails, rather than
// answer nothing, when it cannot find that answer or reads no key from it,
// so the check cannot go blind.
func phoneAuditFeatureKeys(t *testing.T, script string) []string {
	t.Helper()
	routes := phoneAuditFeaturesRoute.FindAllStringIndex(script, -1)
	if len(routes) != 1 {
		t.Fatalf("%s: found %d mocked answers for /api/v1/orgs/{id}/features, want 1: point "+
			"phoneAuditFeaturesRoute at the one the audit has", phoneAudit, len(routes))
	}
	rest := script[routes[0][1]:]
	if end := strings.Index(rest, "\n"); end >= 0 {
		rest = rest[:end]
	}
	m := phoneAuditFeaturesMap.FindStringSubmatch(rest)
	if m == nil {
		t.Fatalf("%s: the mocked answer for /api/v1/orgs/{id}/features has no features: { ... } on its line",
			phoneAudit)
	}
	var keys []string
	for _, entry := range strings.Split(m[1], ",") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		e := phoneAuditFeatureEntry.FindStringSubmatch(entry)
		if e == nil {
			t.Fatalf("%s: cannot read the feature entry %q: write each as 'key': true or false", phoneAudit,
				strings.TrimSpace(entry))
		}
		keys = append(keys, e[1]+e[2]+e[3])
	}
	if len(keys) == 0 {
		t.Fatalf("%s: the mocked answer for /api/v1/orgs/{id}/features turns on no feature", phoneAudit)
	}
	return keys
}

// TestPhoneAuditFeatureKeysReader runs the reader over the shapes of entry
// it accepts, in the answer for the gates and not in another route's.
func TestPhoneAuditFeatureKeysReader(t *testing.T) {
	script := "const routes = [\n" +
		"  [/\\/api\\/v1\\/orgs\\/[^/]+\\/hosted-runner/, { enabled: true }],\n" +
		"  [/\\/api\\/v1\\/orgs\\/[^/]+\\/features/, { channel: 'nightly', features: { 'flow-down': true, " +
		"\"todo-list\": false, notAKey: true } }],\n" +
		"];\n"
	got := phoneAuditFeatureKeys(t, script)
	want := []string{"flow-down", "todo-list", "notAKey"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("phoneAuditFeatureKeys = %q, want %q", got, want)
	}
}
