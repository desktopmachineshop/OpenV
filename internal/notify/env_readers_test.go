package notify

import (
	"os"
	"strings"
)

// The readers the S10 harness (notification_content_harness_test.go and
// notification_content_test.go) builds its channels with: what
// MailerFromEnv, EmailTypesFromEnv and PushTypesFromEnv read before refactor
// step X10b moved the reading of these settings to cmd/server
// (internal/config's SMTP, EmailTypes and PushTypes). They read the
// variables the harness sets, as those did, so the harness is as it was.

// MailerFromEnv is NewMailer over the OPENV_SMTP_* variables.
func MailerFromEnv() *SMTPMailer {
	port := strings.TrimSpace(os.Getenv("OPENV_SMTP_PORT"))
	if port == "" {
		port = "587"
	}
	return NewMailer(SMTPSettings{
		Host:     strings.TrimSpace(os.Getenv("OPENV_SMTP_HOST")),
		Port:     port,
		User:     os.Getenv("OPENV_SMTP_USER"),
		Password: os.Getenv("OPENV_SMTP_PASSWORD"),
		From:     strings.TrimSpace(os.Getenv("OPENV_SMTP_FROM")),
	})
}

// EmailTypesFromEnv is OPENV_EMAIL_NOTIFICATION_TYPES, or DefaultEmailTypes.
func EmailTypesFromEnv() []string {
	return typeListFromEnv("OPENV_EMAIL_NOTIFICATION_TYPES", DefaultEmailTypes)
}

// PushTypesFromEnv is OPENV_PUSH_NOTIFICATION_TYPES, or DefaultPushTypes.
func PushTypesFromEnv() []string {
	return typeListFromEnv("OPENV_PUSH_NOTIFICATION_TYPES", DefaultPushTypes)
}

// typeListFromEnv is a comma-separated list, each item trimmed and the
// empty ones dropped, or fallback() when that leaves none.
func typeListFromEnv(key string, fallback func() []string) []string {
	var out []string
	for _, p := range strings.Split(os.Getenv(key), ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return fallback()
	}
	return out
}
