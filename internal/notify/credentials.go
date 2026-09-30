package notify

import (
	"os"

	"github.com/openv/requirements-platform/internal/envparse"
)

// envSecret reads a credential of the mail and push channels
// (OPENV_SMTP_USER, OPENV_SMTP_PASSWORD, OPENV_VAPID_PRIVATE_KEY) exactly as
// set: never trimmed, with one warning naming the variable, never the
// value, when spaces or a line break sit around it (#379, question 24).
func envSecret(key string) string {
	return envparse.Secret(key, os.Getenv(key))
}

// fromForLog is the From address as the boot log shows it. With no
// OPENV_SMTP_FROM, mail goes out from OPENV_SMTP_USER, a credential, which
// the log names rather than prints (#379, question 24); so does a From
// address that is the SMTP user.
func fromForLog(from, user string) string {
	if from == user {
		return "OPENV_SMTP_USER"
	}
	return from
}
