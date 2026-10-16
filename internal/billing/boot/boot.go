// Package boot builds billing as a registered module (ext.Register), doing
// what stage billing of cmd/server does for the built-in one: for a program
// that links billing into the OpenV server itself, as the hosted service's
// own build does (docs/plans/hosted-edition-separation.md, Phase 3; and
// docs/extending.md). The program reads cfg and limits from its own
// environment (billing.ConfigFromEnv).
package boot

import (
	"log/slog"

	"github.com/openv/requirements-platform/ext"
	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/billing/stripe"
)

// Factory builds the billing module. Off — nothing started, nothing
// dialled, every workspace billing route answering 404 — unless cfg has a
// secret key, and off with a warning on a self-hosted deployment, which
// must never dial a provider.
func Factory(cfg billing.Config, limits billing.Limits) ext.Factory {
	return func(b ext.Boot) (ext.Module, error) {
		var svc *billing.Service
		switch {
		case cfg.Enabled() && ext.SelfHosted():
			slog.Warn("STRIPE_SECRET_KEY is set on a self-hosted deployment (OPENV_SELF_HOSTED=true); billing stays off")
		case cfg.Enabled():
			metrics, _ := b.Metrics().(billing.Metrics)
			opts := []stripe.Option{stripe.WithAPIVersion(cfg.APIVersion)}
			if metrics != nil {
				opts = append(opts, stripe.WithMetrics(metrics))
			}
			provider := stripe.New(cfg.SecretKey, opts...)
			svc = billing.New(provider, b.Workspaces(), cfg.Registry, metrics)
			svc.SetUsers(b.Users())
			svc.SetPortalConfig(cfg.PortalConfig)
			svc.SetTrialDays(cfg.TrialDays)
			svc.SetMaxSeats(cfg.MaxSeats)
			if cfg.ReturnURL != "" {
				svc.SetReturnURL(cfg.ReturnURL)
			}
			svc.Start(b.Context(), cfg.ReconcileInterval)
			slog.Info("billing enabled", "provider", provider.Name(), "prices", cfg.Registry.Len(), "reconcile_every", cfg.ReconcileInterval)
		}
		return billing.NewModule(svc, limits), nil
	}
}
