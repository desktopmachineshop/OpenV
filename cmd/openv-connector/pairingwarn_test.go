package main

import (
	"strings"
	"testing"
)

func TestPairingNeedsConfirmation(t *testing.T) {
	tests := []struct {
		name   string
		apiURL string
		want   bool
	}{
		// https is always fine, regardless of host.
		{"https public host", "https://openv.example.com", false},
		{"https with port and path", "https://openv.example.com:8443/base", false},
		{"https localhost", "https://localhost:8443", false},
		{"https uppercase scheme", "HTTPS://openv.example.com", false},

		// http to loopback is the dev flow — silent.
		{"http localhost", "http://localhost:3000", false},
		{"http localhost no port", "http://localhost", false},
		{"http localhost uppercase host", "http://LOCALHOST:3000", false},
		{"http 127.0.0.1", "http://127.0.0.1:8080", false},
		{"http ipv6 loopback", "http://[::1]:8080", false},

		// http to anything else is cleartext key transfer — confirm.
		{"http public host", "http://openv.example.com", true},
		{"http lan address", "http://192.168.1.20:8080", true},
		{"http other loopback address", "http://127.0.0.2", true},
		{"http localhost lookalike", "http://localhost.evil.example", true},
		{"http localhost subdomain prefix", "http://localhost.example.com:3000", true},

		// Anything that is not provably https+anything or http+loopback.
		{"non-http scheme", "ftp://openv.example.com", true},
		{"missing scheme", "openv.example.com", true},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"unparsable", "http://exa mple.com\x7f", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pairingNeedsConfirmation(tt.apiURL); got != tt.want {
				t.Errorf("pairingNeedsConfirmation(%q) = %v, want %v", tt.apiURL, got, tt.want)
			}
		})
	}
}

func TestConfirmInsecurePairing(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"y", "y\n", true},
		{"yes", "yes\n", true},
		{"uppercase Y", "Y\n", true},
		{"YES with spaces", "  YES  \n", true},
		{"n", "n\n", false},
		{"no", "no\n", false},
		{"empty line defaults to no", "\n", false},
		{"eof defaults to no", "", false},
		{"garbage", "sure\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := confirmInsecurePairing("http://openv.example.com", strings.NewReader(tt.input)); got != tt.want {
				t.Errorf("confirmInsecurePairing with input %q = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
