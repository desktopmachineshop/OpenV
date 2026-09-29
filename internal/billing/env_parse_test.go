package billing

import (
	"os"
	"testing"
)

// TestEnvParse writes internal/billing's sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13). ConfigFromEnv reads its eight variables through the getenv it is
// given (os.Getenv, in cmd/server) and parses each its own way, so there is
// one section per variable: the Config field that variable sets, or the
// error ConfigFromEnv returns, for each input of that variable with the
// other seven unset. A field equal to its value with every variable unset
// shows as default.
func TestEnvParse(t *testing.T) {
	load := func(name string) (Config, error) {
		return ConfigFromEnv(func(key string) string {
			if key == name {
				return os.Getenv(envParseProbe)
			}
			return ""
		})
	}
	base, err := load("")
	if err != nil {
		t.Fatalf("ConfigFromEnv with nothing set: %v", err)
	}
	fields := map[string]struct {
		extra []string
		field func(Config) any
	}{
		"STRIPE_SECRET_KEY":               {nil, func(c Config) any { return c.SecretKey }},
		"OPENV_STRIPE_API_VERSION":        {nil, func(c Config) any { return c.APIVersion }},
		"OPENV_BILLING_RETURN_URL":        {[]string{" https://app.example.test/billing// "}, func(c Config) any { return c.ReturnURL }},
		"OPENV_BILLING_PORTAL_CONFIG":     {nil, func(c Config) any { return c.PortalConfig }},
		"OPENV_BILLING_MAX_SEATS":         {nil, func(c Config) any { return c.MaxSeats }},
		"OPENV_BILLING_TRIAL_DAYS":        {nil, func(c Config) any { return c.TrialDays }},
		"OPENV_BILLING_RECONCILE_MINUTES": {nil, func(c Config) any { return c.ReconcileInterval }},
		"OPENV_STRIPE_PRICES": {[]string{
			` [{"price":"price_b_m","plan":"business","interval":"month"},{"price":"price_bl_y","plan":" business_lite ","interval":"year"}] `,
			`[{"price":"price_e","plan":"enterprise","interval":"month"}]`,
			`[]`,
		}, func(c Config) any { return c.Registry.Entries() }},
	}
	sections := map[string][]string{}
	for name, f := range fields {
		sections["internal/billing:ConfigFromEnv(getenv) "+name] = envParseRows(t, f.extra, func() string {
			cfg, err := load(name)
			if err != nil {
				return "error: " + err.Error()
			}
			return envParseResult(f.field(cfg), f.field(base))
		})
	}
	checkEnvParse(t, "internal/billing", sections)
}
