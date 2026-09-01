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
//
// Flags:
//
//	--insecure   skip the confirmation prompt when the pairing link uses
//	             cleartext http to a non-localhost host (for scripted use)
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

// connectorState is the on-disk config. Pairing keys are per workspace, so
// the connector remembers every workspace it has paired with instead of only
// the most recent one. The legacy flat fields are still read (older configs
// migrate on load) and mirror the active pairing on save, so an older
// connector build pointed at the same file keeps working.
type connectorState struct {
	ActiveOrg string                      `json:"active_org,omitempty"`
	Pairings  map[string]*connectorConfig `json:"pairings,omitempty"`

	APIURL    string `json:"api_url,omitempty"`
	WorkerKey string `json:"worker_key,omitempty"`
	OrgID     string `json:"org_id,omitempty"`
	OrgName   string `json:"org_name,omitempty"`
	UserName  string `json:"user_name,omitempty"`
}

func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "OpenV", "connector.json"), nil
}

func loadState() (*connectorState, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st connectorState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	// Migrate a legacy single-pairing config into the pairings map.
	if len(st.Pairings) == 0 && st.WorkerKey != "" {
		key := st.OrgID
		if key == "" {
			key = "default"
		}
		st.Pairings = map[string]*connectorConfig{key: {
			APIURL:    st.APIURL,
			WorkerKey: st.WorkerKey,
			OrgID:     st.OrgID,
			OrgName:   st.OrgName,
			UserName:  st.UserName,
		}}
		st.ActiveOrg = key
	}
	return &st, nil
}

func saveState(st *connectorState) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Mirror the active pairing into the legacy flat fields.
	if active := st.Pairings[st.ActiveOrg]; active != nil {
		st.APIURL, st.WorkerKey, st.OrgID, st.OrgName, st.UserName =
			active.APIURL, active.WorkerKey, active.OrgID, active.OrgName, active.UserName
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// rememberPairing upserts a workspace pairing and makes it the active one.
func rememberPairing(cfg *connectorConfig) error {
	st, err := loadState()
	if err != nil {
		st = &connectorState{}
	}
	if st.Pairings == nil {
		st.Pairings = map[string]*connectorConfig{}
	}
	key := cfg.OrgID
	if key == "" {
		key = "default"
	}
	st.Pairings[key] = cfg
	st.ActiveOrg = key
	return saveState(st)
}

// selectPairing picks the pairing to start with: the requested workspace when
// the start link names one, else the active (most recently used) pairing.
func selectPairing(orgHint string) (*connectorConfig, error) {
	st, err := loadState()
	if err != nil || len(st.Pairings) == 0 {
		return nil, fmt.Errorf("this connector isn't paired yet.\n  Open OpenV → Workspace settings → Runners → \"Open Agent Connector\" and use the pairing link there.")
	}
	if orgHint != "" {
		if cfg, ok := st.Pairings[orgHint]; ok {
			// Best-effort: remember this as the active workspace for plain starts.
			st.ActiveOrg = orgHint
			_ = saveState(st)
			return cfg, nil
		}
		names := make([]string, 0, len(st.Pairings))
		for _, p := range st.Pairings {
			names = append(names, p.OrgName)
		}
		return nil, fmt.Errorf("this connector isn't paired with that workspace yet — click \"Pair connector\" in OpenV.\n  Paired workspaces: %s", strings.Join(names, ", "))
	}
	if cfg, ok := st.Pairings[st.ActiveOrg]; ok {
		return cfg, nil
	}
	for _, cfg := range st.Pairings {
		return cfg, nil
	}
	return nil, fmt.Errorf("this connector isn't paired yet")
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

	insecure := false
	args := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		if a == "--insecure" {
			insecure = true
			continue
		}
		args = append(args, a)
	}
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}

	// Release builds carry agentd and openv-mcp inside this executable;
	// unpack (or refresh) them next to it before anything might start them.
	// A no-op in plain `go build` builds.
	extractPayload()

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
		handleDeepLink(arg, insecure)
		return
	default:
		start(nil, "")
	}
}

func handleDeepLink(link string, insecure bool) {
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
		cfg := pair(api, code, insecure)
		start(cfg, "")
	case "start":
		// The start link may name the workspace the browser was in, so the
		// runner comes up against the right pairing without re-pairing.
		start(nil, u.Query().Get("org"))
	case "open":
		// One-link flow: start with the existing pairing for the named
		// workspace; only when there is none, pair with the enclosed
		// one-time code first. An unused code simply expires, so opening
		// never rotates a working key.
		org := u.Query().Get("org")
		if cfg, err := selectPairing(org); err == nil {
			start(cfg, "")
			return
		}
		code := u.Query().Get("code")
		api := u.Query().Get("api")
		if code == "" || api == "" {
			fail("this connector isn't paired with that workspace and the link carries no pairing code — click \"Open connector\" in OpenV to get a fresh one")
		}
		start(pair(api, code, insecure), "")
	default:
		// A link this build does not recognise means the connector on disk is
		// older than the OpenV that produced the link — the usual cause is a
		// copy downloaded before the action existed, still registered as the
		// openv-connector:// handler. Do the useful thing rather than
		// dead-ending: with a pairing on hand, starting the runner is almost
		// certainly what the click was for.
		if cfg, err := selectPairing(u.Query().Get("org")); err == nil {
			fmt.Printf("  This connector does not understand %q — it is older than your OpenV.\n", action)
			fmt.Println("  Starting the runner with the existing pairing instead.")
			fmt.Println("  Download a fresh connector from OpenV (Runners page) and run it once to update the link.")
			start(cfg, "")
			return
		}
		fail("this connector is out of date: it does not understand %q, and it is not paired with that workspace.\n"+
			"  Download a fresh connector from OpenV (Runners page) and run it once — that re-registers the link — then try again", action)
	}
}

// pair exchanges a one-time code for this member's personal runner key.
func pair(apiURL, code string, insecure bool) *connectorConfig {
	if pairingNeedsConfirmation(apiURL) {
		if insecure {
			fmt.Printf("  Warning: pairing with %s over cleartext http (--insecure given, continuing).\n", apiURL)
		} else if !confirmInsecurePairing(apiURL, os.Stdin) {
			fail("pairing cancelled — the link does not use HTTPS.\n  Use an https:// OpenV address, or re-run with --insecure to accept the risk.")
		}
	}
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
	if err := rememberPairing(cfg); err != nil {
		fail("could not save connector config: %v", err)
	}
	fmt.Printf("  Paired with workspace %q. Config saved.\n", cfg.OrgName)
	return cfg
}

// start launches agentd with the stored personal key, attached to this
// console. Closing the window stops the runner. orgHint selects among the
// stored per-workspace pairings when set.
func start(cfg *connectorConfig, orgHint string) {
	if cfg == nil {
		selected, err := selectPairing(orgHint)
		if err != nil {
			fail("%v", err)
		}
		cfg = selected
	}

	agentd, err := findAgentd()
	if err != nil {
		fail("%v", err)
	}
	mcp := filepath.Join(filepath.Dir(agentd), mcpBinaryName())

	fmt.Printf("  Workspace: %s\n", cfg.OrgName)
	fmt.Printf("  API:       %s\n", cfg.APIURL)
	if !probeHealth(cfg.APIURL, healthProbeTimeout) {
		fail("could not reach OpenV at %s.\n  If the server is up, this pairing is probably stale — open OpenV in the browser and click \"Pair connector\" to re-pair this workspace.", cfg.APIURL)
	}
	warnCleartextStart(cfg.APIURL, os.Stdout)
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
