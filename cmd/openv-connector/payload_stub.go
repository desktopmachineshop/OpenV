//go:build !embedpayload

package main

// extractPayload is a no-op in builds without an embedded runner payload (the
// default `go build`): agentd and openv-mcp are expected next to the
// connector, as when built with `make worker`. Release downloads are built
// with -tags embedpayload and carry both binaries inside the executable.
func extractPayload() {}
