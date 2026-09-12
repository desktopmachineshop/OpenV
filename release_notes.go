// Package openv is the repository root. It exists to embed the files that
// live at the root because people read them there — today the customer-facing
// release notes, which the API serves and announces.
package openv

import _ "embed"

// ReleaseNotesMarkdown is RELEASE_NOTES.md as built into the binary. The
// server parses it at boot (internal/domain/release) to learn which release
// it is and what to tell members about it.
//
//go:embed RELEASE_NOTES.md
var ReleaseNotesMarkdown string
