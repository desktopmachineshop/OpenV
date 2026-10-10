package billing

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// The registry is the only thing that turns a price into a plan. It refuses
// anything that could sell a plan nobody buys, or leave "which price wins"
// to map order.
func TestParseRegistryAcceptsTheFourPrices(t *testing.T) {
	r, err := ParseRegistry(`[
		{"price":"price_lm","plan":"business_lite","interval":"month"},
		{"price":"price_ly","plan":"business_lite","interval":"year"},
		{"price":"price_bm","plan":"business","interval":"month"},
		{"price":"price_by","plan":"business","interval":"year"}
	]`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Len() != 4 {
		t.Fatalf("Len = %d", r.Len())
	}
	e, ok := r.Lookup("price_by")
	if !ok || e.Plan != orgs.PlanBusiness || e.Interval != IntervalYear {
		t.Fatalf("Lookup(price_by) = %+v, %v", e, ok)
	}
	if _, ok := r.Lookup("price_unknown"); ok {
		t.Fatal("an unknown price resolved")
	}
	entries := r.Entries()
	if entries[0].Plan != orgs.PlanBusiness || entries[0].Interval != IntervalMonth {
		t.Fatalf("entries are not ordered plan then interval: %+v", entries)
	}
}

func TestParseRegistryRefusesWhatItCannotSell(t *testing.T) {
	cases := map[string]string{
		"not json":           `{"price":"x"}`,
		"enterprise":         `[{"price":"p","plan":"enterprise","interval":"month"}]`,
		"open source":        `[{"price":"p","plan":"open_source","interval":"month"}]`,
		"self host":          `[{"price":"p","plan":"self_host","interval":"month"}]`,
		"legacy alias team":  `[{"price":"p","plan":"team","interval":"month"}]`,
		"free":               `[{"price":"p","plan":"single","interval":"month"}]`,
		"bad interval":       `[{"price":"p","plan":"business","interval":"week"}]`,
		"no price":           `[{"price":"","plan":"business","interval":"month"}]`,
		"price listed twice": `[{"price":"p","plan":"business","interval":"month"},{"price":"p","plan":"business","interval":"year"}]`,
		"two prices for one": `[{"price":"p1","plan":"business","interval":"month"},{"price":"p2","plan":"business","interval":"month"}]`,
	}
	for name, raw := range cases {
		if _, err := ParseRegistry(raw); err == nil {
			t.Errorf("%s: accepted %s", name, raw)
		}
	}
	// Nothing configured is an empty registry, not an error: billing may
	// be on with nothing yet for sale.
	r, err := ParseRegistry("  ")
	if err != nil || r.Len() != 0 {
		t.Fatalf("empty: %v %d", err, r.Len())
	}
	var nilReg *Registry
	if _, ok := nilReg.Lookup("p"); ok || nilReg.Len() != 0 || nilReg.Entries() != nil {
		t.Fatal("a nil registry should be empty and safe")
	}
}

func TestMapStatus(t *testing.T) {
	want := map[string]string{
		"trialing": orgs.PlanStatusTrialing, "active": orgs.PlanStatusActive, "past_due": orgs.PlanStatusPastDue,
		"unpaid": orgs.PlanStatusUnpaid, "paused": orgs.PlanStatusPaused, "canceled": orgs.PlanStatusCanceled,
		"incomplete": orgs.PlanStatusIncomplete, "incomplete_expired": orgs.PlanStatusIncomplete,
		"something_new": "something_new",
	}
	for in, out := range want {
		if got := MapStatus(in); got != out {
			t.Errorf("MapStatus(%q) = %q, want %q", in, got, out)
		}
	}
	// Whatever a provider invents, the workspace is not entitled to a paid
	// plan on the strength of it.
	o := &orgs.Org{BilledPlan: orgs.PlanBusiness, Billing: orgs.Billing{Status: MapStatus("something_new")}}
	if o.EntitledPlan() != orgs.PlanSingle {
		t.Error("an unknown provider status kept the paid plan")
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	cfg, err := ConfigFromEnv(env(map[string]string{}))
	if err != nil || cfg.Enabled() || cfg.Registry.Len() != 0 || cfg.ReconcileInterval != 5*time.Minute {
		t.Fatalf("empty env: %+v %v", cfg, err)
	}
	cfg, err = ConfigFromEnv(env(map[string]string{
		"STRIPE_SECRET_KEY":               " sk_test_fake_for_unit_tests ",
		"OPENV_STRIPE_PRICES":             `[{"price":"p","plan":"business","interval":"month"}]`,
		"OPENV_BILLING_RECONCILE_MINUTES": "15",
		"OPENV_STRIPE_API_VERSION":        "2025-08-27.basil",
	}))
	if err != nil || !cfg.Enabled() || cfg.Registry.Len() != 1 || cfg.ReconcileInterval != 15*time.Minute || cfg.APIVersion == "" {
		t.Fatalf("full env: %+v %v", cfg, err)
	}
	// The key is a credential, used exactly as set (#379, question 24): the
	// spaces round it stay, where it used to be trimmed.
	if cfg.SecretKey != " sk_test_fake_for_unit_tests " {
		t.Fatalf("key not exactly as set: %q", cfg.SecretKey)
	}
	if _, err := ConfigFromEnv(env(map[string]string{"OPENV_STRIPE_PRICES": `[{"price":"p","plan":"enterprise","interval":"month"}]`})); err == nil || !strings.Contains(err.Error(), "OPENV_STRIPE_PRICES") {
		t.Fatalf("a bad price map should name the variable: %v", err)
	}
	if _, err := ConfigFromEnv(env(map[string]string{"OPENV_BILLING_RECONCILE_MINUTES": "soon"})); err == nil {
		t.Fatal("a bad interval was accepted")
	}
}

// secretKeyRuns numbers the runs of
// TestConfigFromEnvKeepsTheSecretKeyExactlyAsSet in this process.
var secretKeyRuns atomic.Int64

// The secret key is a credential, used exactly as set (#379, question 24):
// a line break after it stays, with one warning naming STRIPE_SECRET_KEY and
// never the key, where it used to be trimmed off in silence. A key of only
// spaces still leaves billing off, as it always did.
func TestConfigFromEnvKeepsTheSecretKeyExactlyAsSet(t *testing.T) {
	// A key no earlier run of this test (go test -count=2) set:
	// internal/envparse warns once per variable and value for the life of
	// the process.
	key := fmt.Sprintf("sk_test_do_not_log_run%d\n", secretKeyRuns.Add(1))
	var log bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg, err := ConfigFromEnv(envOf(map[string]string{"STRIPE_SECRET_KEY": key}))
	if err != nil || cfg.SecretKey != key || !cfg.Enabled() {
		t.Fatalf("key %q, enabled %v, err %v: want the key exactly as set, and billing on", cfg.SecretKey, cfg.Enabled(), err)
	}
	if !strings.Contains(log.String(), "used exactly as set") || !strings.Contains(log.String(), "var=STRIPE_SECRET_KEY") {
		t.Errorf("no warning naming STRIPE_SECRET_KEY:\n%s", log.String())
	}
	if strings.Contains(log.String(), "sk_test_do_not_log") {
		t.Errorf("the warning printed the key:\n%s", log.String())
	}
	cfg, err = ConfigFromEnv(envOf(map[string]string{"STRIPE_SECRET_KEY": "   "}))
	if err != nil || cfg.SecretKey != "   " || cfg.Enabled() {
		t.Fatalf("key %q, enabled %v, err %v: want it as set, and billing off", cfg.SecretKey, cfg.Enabled(), err)
	}
}

// envOf is a getenv over a fixed set of variables.
func envOf(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// Billing still refuses to start on a malformed count, but a count is now a
// whole number and nothing else (#379, question 15): Sscanf("%d") read the
// number at the front of 30d, 7x, 1e3 and 2.5 and dropped the rest, so a
// trial meant as a month ran 30 days by luck and 1e3 seats meant one. Each
// is refused, naming the variable and saying it must be a whole number.
func TestConfigFromEnvRefusesACountWithAnythingAfterIt(t *testing.T) {
	counts := map[string]string{
		"OPENV_BILLING_MAX_SEATS":         "seats",
		"OPENV_BILLING_TRIAL_DAYS":        "days",
		"OPENV_BILLING_RECONCILE_MINUTES": "minutes",
	}
	for name, unit := range counts {
		for _, raw := range []string{"30d", "7x", "1e3", "2.5", "2h", "0x10", "-5", "TRUE"} {
			_, err := ConfigFromEnv(envOf(map[string]string{name: raw}))
			want := fmt.Sprintf("%s must be a whole number of %s, got %q", name, unit, raw)
			if err == nil || err.Error() != want {
				t.Errorf("%s=%q: err = %v, want %q", name, raw, err, want)
			}
		}
		// Spaces round a whole number, and its sign, are not junk.
		if _, err := ConfigFromEnv(envOf(map[string]string{name: " +7 "})); err != nil {
			t.Errorf("%s=%q: %v", name, " +7 ", err)
		}
	}
	cfg, err := ConfigFromEnv(envOf(map[string]string{
		"OPENV_BILLING_MAX_SEATS": " 40 ", "OPENV_BILLING_TRIAL_DAYS": "30", "OPENV_BILLING_RECONCILE_MINUTES": "+7",
	}))
	if err != nil || cfg.MaxSeats != 40 || cfg.TrialDays != 30 || cfg.ReconcileInterval != 7*time.Minute {
		t.Fatalf("whole numbers: %+v %v", cfg, err)
	}
	// Trial days alone may be 0: no trial.
	if cfg, err := ConfigFromEnv(envOf(map[string]string{"OPENV_BILLING_TRIAL_DAYS": "0"})); err != nil || cfg.TrialDays != 0 {
		t.Fatalf("OPENV_BILLING_TRIAL_DAYS=0: %+v %v", cfg, err)
	}
	for _, name := range []string{"OPENV_BILLING_MAX_SEATS", "OPENV_BILLING_RECONCILE_MINUTES"} {
		if _, err := ConfigFromEnv(envOf(map[string]string{name: "0"})); err == nil {
			t.Errorf("%s=0 was accepted", name)
		}
	}
}

// The price map's error says what shape it expects, in the map's own terms,
// where the decoder's words named the Go type []billing.PriceEntry (#379,
// question 15), which an operator never wrote and a move of the package
// would change.
func TestParseRegistryNamesTheShapeNotAGoType(t *testing.T) {
	cases := map[string]string{
		`7`:                     "the value is a number, not an array",
		`true`:                  "the value is true or false, not an array",
		`"business"`:            "the value is a string, not an array",
		`{"price":"p"}`:         "the value is an object, not an array",
		`["price_p"]`:           "an entry is a string, not an object",
		`[[1]]`:                 "an entry is an array, not an object",
		`[{"price":5}]`:         "an entry's price is a number, not a string",
		`[{"plan":{"a":1}}]`:    "an entry's plan is an object, not a string",
		`[{"interval":false}]`:  "an entry's interval is true or false, not a string",
		`business_lite:month=p`: "invalid character 'b' looking for beginning of value",
	}
	for raw, want := range cases {
		_, err := ParseRegistry(raw)
		if err == nil || err.Error() != "not a JSON array of {price, plan, interval}: "+want {
			t.Errorf("ParseRegistry(%s) = %v, want %q", raw, err, want)
		}
		if err != nil && (strings.Contains(err.Error(), "PriceEntry") || strings.Contains(err.Error(), "Go ")) {
			t.Errorf("ParseRegistry(%s) names a Go type: %v", raw, err)
		}
	}
}
