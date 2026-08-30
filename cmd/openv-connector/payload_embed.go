//go:build embedpayload

package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// Release builds embed agentd and openv-mcp so the download is one file. The
// build step compiles both for the target OS into payload/ under OS-neutral
// names before building the connector with -tags embedpayload (see
// Dockerfile.api and the connector-dist make target).

//go:embed payload/agentd
var agentdPayload []byte

//go:embed payload/openv-mcp
var mcpPayload []byte

// extractPayload unpacks the embedded runner binaries next to the connector
// executable — where findAgentd looks — creating or replacing them whenever
// their content differs (a downloaded update refreshes stale copies).
// Failures warn rather than abort: an already-extracted set keeps working.
func extractPayload() {
	self, err := os.Executable()
	if err != nil {
		fmt.Printf("  Warning: could not locate the connector executable to unpack the runner: %v\n", err)
		return
	}
	dir := filepath.Dir(self)
	wrote := false
	for _, p := range []struct {
		name    string
		content []byte
	}{
		{agentdBinaryName(), agentdPayload},
		{mcpBinaryName(), mcpPayload},
	} {
		changed, err := ensurePayloadFile(dir, p.name, p.content)
		if err != nil {
			fmt.Printf("  Warning: could not unpack %s: %v\n", p.name, err)
			continue
		}
		if changed {
			wrote = true
		}
	}
	if wrote {
		fmt.Println("  Unpacked runner components (agentd, openv-mcp) next to the connector.")
	}
}
