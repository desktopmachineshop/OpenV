// Command openv-vapid prints a fresh VAPID key pair for web push (REQ-109).
//
// Web push needs one application server key pair per DEPLOYMENT, not per
// user: the public half is handed to browsers as the applicationServerKey
// they subscribe with, and the private half signs the request to the push
// service. Rotating the pair invalidates every existing subscription, so
// generate once and keep the private key with the rest of the deployment's
// secrets.
//
// Usage:
//
//	make vapid-keys           # or: go run ./cmd/openv-vapid
//
// The output is three dotenv lines ready to paste into the API service's
// environment; edit OPENV_VAPID_SUBJECT to a contact address you own — push
// services use it to reach the operator about a misbehaving deployment, and
// the server refuses anything that is not a mailto: or https: URI.
package main

import (
	"fmt"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func main() {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to generate VAPID keys:", err)
		os.Exit(1)
	}
	fmt.Printf("OPENV_VAPID_PUBLIC_KEY=%s\n", public)
	fmt.Printf("OPENV_VAPID_PRIVATE_KEY=%s\n", private)
	fmt.Println("OPENV_VAPID_SUBJECT=mailto:admin@example.com")
}
