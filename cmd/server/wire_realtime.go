package main

import (
	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/metrics"
	"github.com/openv/requirements-platform/internal/orchestration"
)

// realtime builds the metrics collector, the SSE hub and the orchestration
// hooks, and subscribes them.
func (a *app) realtime() {
	// Prometheus metrics. The collector subscribes to run lifecycle events so
	// the run counter and queued/running gauges track transitions, and reports
	// live SSE connection counts on scrape. Wired below at /metrics.
	a.metricsCollector = metrics.New()
	a.runService.AddSubscriber(a.metricsCollector)

	// SSE hub + orchestration hooks.
	a.sseHub = api.NewSSEHub()
	a.runService.AddSubscriber(a.sseHub)
	a.metricsCollector.WatchSSEConnections(a.sseHub.ActiveConnections)
	a.hooks = orchestration.NewHooks(a.runService, a.teamService, a.workItemService, a.interviewService, a.guidedService, a.projectService, a.sseHub, handoffReach(a.projectService, a.orgService, a.memberService))
	a.runService.AddSubscriber(a.hooks)
	a.hooks.SubscribeBus(a.bus)
}
