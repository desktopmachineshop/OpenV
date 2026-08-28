// openv-connector is the OpenV Agent Connector: a small launcher the browser
// can invoke via the openv-connector:// protocol to start a personal runner
// on demand. It is deliberately NOT a background service — it runs only when
// opened, in a visible console window, and stops when that window closes.
//
// Usage:
//
//	openv-connector install                 register the protocol handler
//	openv-connector uninstall               remove the protocol handler
//	openv-connector openv-connector://pair?code=..&api=..   pair + start
//	openv-connector openv-connector://start                  start
//	openv-connector                          start (default)
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type connectorConfig struct {
	APIURL    string `json:"api_url"`
	WorkerKey string `json:"worker_key"`
	OrgID     string `json:"org_id"`
	OrgName   string `json:"org_name"`
	UserName  string `json:"user_name"`
}

func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "OpenV", "connector.json"), nil
}

func loadConfig() (*connectorConfig, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg connectorConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveConfig(cfg *connectorConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// fail prints the error and keeps the window open so the user can read it.
func fail(format string, args ...interface{}) {
	fmt.Printf("\n  ERROR: "+format+"\n\n", args...)
	fmt.Print("  Press Enter to close this window...")
	bufio.NewReader(os.Stdin).ReadString('\n')
	os.Exit(1)
}

func main() {
	fmt.Println()
	fmt.Println("  OpenV Agent Connector")
	fmt.Println("  ---------------------")

	arg := ""
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}

	// Best-effort self-registration on every launch (HKCU, idempotent), so a
	// first double-click is enough to make openv-connector:// links work.
	if arg != "uninstall" {
		_ = registerProtocol()
	}

	switch {
	case arg == "install":
		if err := registerProtocol(); err != nil {
			fail("could not register the openv-connector:// protocol: %v", err)
		}
		fmt.Println("  Protocol handler registered. OpenV can now open this connector from the browser.")
		return
	case arg == "uninstall":
		if err := unregisterProtocol(); err != nil {
			fail("could not remove the protocol handler: %v", err)
		}
		fmt.Println("  Protocol handler removed.")
		return
	case strings.HasPrefix(arg, "openv-connector://"):
		handleDeepLink(arg)
		return
	default:
		start(nil)
	}
}

func handleDeepLink(link string) {
	u, err := url.Parse(link)
	if err != nil {
		fail("invalid link: %v", err)
	}
	action := u.Host
	if action == "" {
		action = strings.Trim(u.Path, "/")
	}
	switch action {
	case "pair":
		code := u.Query().Get("code")
		api := u.Query().Get("api")
		if code == "" || api == "" {
			fail("pairing link is missing its code — create a fresh one from the OpenV Runners page")
		}
		cfg := pair(api, code)
		start(cfg)
	case "start":
		start(nil)
	default:
		fail("unknown action %q", action)
	}
}

// pair exchanges a one-time code for this member's personal runner key.
func pair(apiURL, code string) *connectorConfig {
	fmt.Printf("  Pairing with %s ...\n", apiURL)
	body, _ := json.Marshal(map[string]string{"code": code})
	resp, err := http.Post(strings.TrimRight(apiURL, "/")+"/api/v1/public/connector/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		fail("could not reach OpenV at %s: %v", apiURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg := new(bytes.Buffer)
		msg.ReadFrom(resp.Body)
		fail("pairing failed: %s", strings.TrimSpace(msg.String()))
	}
	var out struct {
		WorkerKey string `json:"worker_key"`
		APIURL    string `json:"api_url"`
		OrgID     string `json:"org_id"`
		OrgName   string `json:"org_name"`
		UserName  string `json:"user_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		fail("unexpected pairing response: %v", err)
	}
	cfg := &connectorConfig{
		APIURL:    out.APIURL,
		WorkerKey: out.WorkerKey,
		OrgID:     out.OrgID,
		OrgName:   out.OrgName,
		UserName:  out.UserName,
	}
	// Prefer the address the browser actually used when the server's idea
	// of its public URL isn't reachable from here.
	chosen, reason := resolveAPIURL(cfg.APIURL, apiURL, func(candidate string) bool {
		return probeHealth(candidate, healthProbeTimeout)
	})
	if reason != "" {
		fmt.Printf("  Note: %s (%s).\n", reason, chosen)
	}
	cfg.APIURL = chosen
	if err := saveConfig(cfg); err != nil {
		fail("could not save connector config: %v", err)
	}
	fmt.Printf("  Paired with workspace %q. Config saved.\n", cfg.OrgName)
	return cfg
}

// start launches agentd with the stored personal key, attached to this
// console. Closing the window stops the runner.
func start(cfg *connectorConfig) {
	if cfg == nil {
		loaded, err := loadConfig()
		if err != nil {
			fail("this connector isn't paired yet.\n  Open OpenV → Workspace settings → Runners → \"Open Agent Connector\" and use the pairing link there.")
		}
		cfg = loaded
	}

	agentd, err := findAgentd()
	if err != nil {
		fail("%v", err)
	}
	mcp := filepath.Join(filepath.Dir(agentd), mcpBinaryName())

	fmt.Printf("  Workspace: %s\n", cfg.OrgName)
	fmt.Printf("  API:       %s\n", cfg.APIURL)
	fmt.Println("  Your personal runner is starting — leave this window open while agents work.")
	fmt.Println("  Close this window (or press Ctrl+C) to stop it.")
	fmt.Println()

	// The worker key goes through the environment, not argv: command lines
	// are visible to every process on the machine (ps, Task Manager), the
	// environment of a child we spawn is not. agentd reads WORKER_API_KEY as
	// the default for its --worker-key flag.
	cmd := exec.Command(agentd, "--api", cfg.APIURL, "--mcp-binary", mcp)
	cmd.Env = append(os.Environ(), "WORKER_API_KEY="+cfg.WorkerKey)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	if err := cmd.Start(); err != nil {
		fail("could not start agentd: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-interrupt:
		_ = cmd.Process.Kill()
		<-done
	case err := <-done:
		if err != nil {
			fail("the runner exited: %v", err)
		}
	}
	// Brief pause so a fast exit is readable.
	time.Sleep(500 * time.Millisecond)
}

func agentdBinaryName() string {
	if runtime.GOOS == "windows" {
		return "agentd.exe"
	}
	return "agentd"
}

func mcpBinaryName() string {
	if runtime.GOOS == "windows" {
		return "openv-mcp.exe"
	}
	return "openv-mcp"
}

// findAgentd looks for agentd next to this executable.
func findAgentd() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not locate the connector executable: %v", err)
	}
	candidate := filepath.Join(filepath.Dir(self), agentdBinaryName())
	if _, err := os.Stat(candidate); err != nil {
		return "", fmt.Errorf("agentd not found next to the connector (%s) — reinstall the connector bundle", candidate)
	}
	return candidate, nil
}
