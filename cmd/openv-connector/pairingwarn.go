package main

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// pairingNeedsConfirmation reports whether pairing against apiURL must be
// confirmed by the user first. Pairing posts the one-time code and receives
// the worker key over whatever scheme the link uses, so anything other than
// https means the key travels in cleartext. Plain http is accepted silently
// only for loopback hosts (localhost, 127.0.0.1, ::1) — the local dev flow.
// A URL we cannot parse is treated as needing confirmation: we cannot prove
// it is safe, and the pairing request itself will surface the real problem.
func pairingNeedsConfirmation(apiURL string) bool {
	u, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil {
		return true
	}
	if strings.EqualFold(u.Scheme, "https") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return false
	}
	return true
}

// warnCleartextStart prints a warning to w when starting against apiURL would
// send the worker key in cleartext — the same decision pairing uses
// (pairingNeedsConfirmation). Unlike pairing, start may be non-interactive
// (protocol handler, scripts), so it only warns and never prompts. It reports
// whether a warning was printed.
func warnCleartextStart(apiURL string, w io.Writer) bool {
	if !pairingNeedsConfirmation(apiURL) {
		return false
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  WARNING: the stored API address %s is not HTTPS.\n", strings.TrimSpace(apiURL))
	fmt.Fprintln(w, "  Your worker key travels unencrypted on every run and could be read or")
	fmt.Fprintln(w, "  altered by anyone on the network path. Re-pair from the OpenV Runners")
	fmt.Fprintln(w, "  page using an https:// address to fix this.")
	fmt.Fprintln(w)
	return true
}

// confirmInsecurePairing warns that apiURL is not HTTPS and asks for an
// explicit y/N answer on in (stdin in real use). Only "y"/"yes" — any case —
// counts as consent; anything else, including EOF, declines.
func confirmInsecurePairing(apiURL string, in io.Reader) bool {
	fmt.Println()
	fmt.Printf("  WARNING: the pairing link uses %s — not HTTPS.\n", apiURL)
	fmt.Println("  The one-time code and your worker key would travel unencrypted and could")
	fmt.Println("  be read or altered by anyone on the network path.")
	fmt.Print("  Continue anyway? [y/N] ")
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && answer == "" {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
