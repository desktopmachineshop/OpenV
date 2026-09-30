package main

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"regexp"
	"testing"
)

// cliName is the command cli_harness_test.go builds and runs.
const cliName = "openv-vapid"

// cliKeyLine is one line of openv-vapid's output that carries a key.
var cliKeyLine = regexp.MustCompile(`(?m)^(OPENV_VAPID_PUBLIC_KEY|OPENV_VAPID_PRIVATE_KEY)=(.*)$`)

// TestCLI snapshots openv-vapid's output (refactor plan S8, invariant I14):
// the three dotenv keys it prints, not the random key pair. Each run checks
// the pair instead (base64url without padding; a 32-byte P-256 private key
// whose public key is the 65-byte uncompressed point printed) and puts a
// placeholder in its place; a key that fails the check stays raw, so the
// golden shows it. openv-vapid takes no flags and ignores its arguments.
func TestCLI(t *testing.T) {
	runCLI(t, []cliScenario{
		{name: "keys", normalise: cliVAPIDKeys},
		{name: "keys_ignores_arguments", args: []string{"-h", "--x"}, normalise: cliVAPIDKeys},
	})
}

// cliVAPIDKeys checks the key pair in stdout and replaces both keys.
func cliVAPIDKeys(t *testing.T, stdout string) string {
	keys := map[string][]byte{}
	for _, m := range cliKeyLine.FindAllStringSubmatch(stdout, -1) {
		raw, err := base64.RawURLEncoding.DecodeString(m[2])
		if err != nil {
			t.Errorf("%s is not unpadded base64url: %v", m[1], err)
			continue
		}
		keys[m[1]] = raw
	}
	pub, priv := keys["OPENV_VAPID_PUBLIC_KEY"], keys["OPENV_VAPID_PRIVATE_KEY"]
	key, err := ecdh.P256().NewPrivateKey(priv)
	if err != nil || len(pub) != 65 || pub[0] != 4 || !bytes.Equal(key.PublicKey().Bytes(), pub) {
		t.Errorf("the printed keys are not a P-256 key pair (private key error %v); the golden keeps them raw", err)
		return stdout
	}
	return cliKeyLine.ReplaceAllStringFunc(stdout, func(line string) string {
		if m := cliKeyLine.FindStringSubmatch(line); m[1] == "OPENV_VAPID_PUBLIC_KEY" {
			return m[1] + "=<base64url: the 65-byte uncompressed P-256 public key of the private key below>"
		}
		return "OPENV_VAPID_PRIVATE_KEY=<base64url: a 32-byte P-256 private key>"
	})
}
