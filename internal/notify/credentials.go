package notify

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
