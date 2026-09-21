package billing

import (
	"strings"
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
	if cfg.SecretKey != "sk_test_fake_for_unit_tests" {
		t.Fatalf("key not trimmed: %q", cfg.SecretKey)
	}
	if _, err := ConfigFromEnv(env(map[string]string{"OPENV_STRIPE_PRICES": `[{"price":"p","plan":"enterprise","interval":"month"}]`})); err == nil || !strings.Contains(err.Error(), "OPENV_STRIPE_PRICES") {
		t.Fatalf("a bad price map should name the variable: %v", err)
	}
	if _, err := ConfigFromEnv(env(map[string]string{"OPENV_BILLING_RECONCILE_MINUTES": "soon"})); err == nil {
		t.Fatal("a bad interval was accepted")
	}
}
