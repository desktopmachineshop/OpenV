package projects

// Repository defines the project repository interface
type Repository interface {
	Create(project *Project) error
	GetByID(id string) (*Project, error)
	GetAll() ([]*Project, error)
	// ListByOrg returns the projects in one org. It fails closed: an empty
	// orgID matches no rows (never every project), so a caller that could not
	// resolve an active workspace cannot leak cross-tenant projects.
	ListByOrg(orgID string) ([]*Project, error)
	Update(project *Project) error
	Delete(id string) error
}
