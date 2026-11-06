package orgs

// Notices is the slice of Service for the once-only notices a workspace's
// admins are sent by email: a module's message about the workspace (the
// hosted service's billing emails, docs/extending.md), keyed by the
// occurrence it is about. The claim is what makes a notice go out once
// however often a polling loop sees its cause.
type Notices interface {
	// ClaimNotice records that the notice key is being sent and reports
	// whether this caller won the claim; see Repository.ClaimNotice.
	ClaimNotice(orgID, key, subject string) (bool, error)
	// RecordNotice writes how many admins a claimed notice reached.
	RecordNotice(orgID, key string, recipients int) error
	// ReleaseNotice forgets a claim whose notice reached nobody.
	ReleaseNotice(orgID, key string) error
}

// ClaimNotice delegates the once-per-key claim to the repository.
func (s *DefaultService) ClaimNotice(orgID, key, subject string) (bool, error) {
	return s.repo.ClaimNotice(orgID, key, subject)
}

// RecordNotice delegates to the repository.
func (s *DefaultService) RecordNotice(orgID, key string, recipients int) error {
	return s.repo.RecordNotice(orgID, key, recipients)
}

// ReleaseNotice delegates to the repository.
func (s *DefaultService) ReleaseNotice(orgID, key string) error {
	return s.repo.ReleaseNotice(orgID, key)
}
