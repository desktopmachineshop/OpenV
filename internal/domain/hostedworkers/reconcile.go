package hostedworkers

import "log/slog"

// ContainerInspector reports the state of a hosted runner's container as the
// container runtime names it ("running", "exited", "missing", ...).
// internal/hosting's Provisioner is one; cmd/server passes it in, since a
// domain package imports no infrastructure (K7).
type ContainerInspector interface {
	ContainerState(containerName string) (string, error)
}

// Reconcile brings each stored hosted runner's status in line with its
// container's state, at boot: "missing" is an error with the detail
// "container not found"; "running" is running; "exited", "created",
// "paused" and "dead" are stopped; running and stopped clear the detail. Any
// other state leaves the record as it is (there is no default branch), and a
// record that already says what the state means is not written. The
// runners are inspected one by one in list order. A failure is logged and
// skips that runner, or all of them when the list cannot be read.
func Reconcile(provisioner ContainerInspector, hostedWorkerService Service) {
	if hostedList, err := hostedWorkerService.ListAll(); err != nil {
		slog.Warn("failed to list hosted workers for reconcile", "error", err)
	} else {
		for _, hw := range hostedList {
			state, err := provisioner.ContainerState(hw.ContainerName)
			if err != nil {
				slog.Warn("failed to inspect hosted runner", "container", hw.ContainerName, "error", err)
				continue
			}
			status, detail := hw.Status, hw.Detail
			switch state {
			case "missing":
				status, detail = StatusError, "container not found"
			case "running":
				status, detail = StatusRunning, ""
			case "exited", "created", "paused", "dead":
				status, detail = StatusStopped, ""
			}
			if status != hw.Status || detail != hw.Detail {
				if _, err := hostedWorkerService.SetStatus(hw.ID, status, detail); err != nil {
					slog.Warn("failed to reconcile hosted runner", "container", hw.ContainerName, "error", err)
				}
			}
		}
	}
}
