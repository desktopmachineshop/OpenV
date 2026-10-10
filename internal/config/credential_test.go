package config

import "github.com/openv/requirements-platform/internal/envparse"

// credential reads a credential as secret does, but with no default: ""
// when it is unset or empty. It is notify's envSecret, which had none, for
// the mail and push channels' credentials (OPENV_SMTP_USER,
// OPENV_SMTP_PASSWORD, OPENV_VAPID_PRIVATE_KEY), which refactor step X10b
// reads through it so that their S8 rows keep no default. X10b moves it into
// config.go; until then it is declared here, for TestEnvParse to name, which
// writes its section only once S8's inventory lists it.
func (c *Config) credential(name string) string {
	return envparse.Secret(name, c.getenv(name))
}
