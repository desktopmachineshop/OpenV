package ext

import "context"

// Notice is an email a module sends a workspace's admins once: the hosted
// service's billing emails (a renewal that failed, a trial about to end) are
// notices.
type Notice struct {
	// Key names the occurrence the notice is about, such as
	// "billing:payment_failed:<subscription>:<period>". The server records
	// it, and a second notice under one key to one workspace is not sent,
	// however often the module asks. Prefix it with the module's name; at
	// most 200 bytes.
	Key string
	// Subject is the email's subject line.
	Subject string
	// Body is plain text. The server puts a greeting before it, then the
	// link to Path, then a line saying why the admin got it.
	Body string
	// Path is an app path, such as "/org/settings?tab=billing", sent as a
	// full link on the app's origin; "" for none.
	Path string
}

// Mail is the server's own mail (OPENV_SMTP_*), lent to modules for
// notices.
type Mail interface {
	// Enabled reports whether this deployment sends email at all.
	Enabled() bool
	// SendToAdmins emails n to every admin of the workspace orgID unless a
	// notice under n.Key was already sent there, and reports whether this
	// call sent it. With mail off, or no admin with an address, nothing is
	// sent or recorded. A notice that reached no admin is not recorded
	// either, so the next call tries again; one that reached some is never
	// sent again, and err names the failures.
	SendToAdmins(ctx context.Context, orgID string, n Notice) (sent bool, err error)
}

// MailBoot is the part of Boot that lends a module the server's mail. The
// server's Boot implements it; a module asserts it, so a Boot of a module's
// own tests need not.
type MailBoot interface {
	Mail() Mail
}
