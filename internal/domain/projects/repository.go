package projects

// Repository defines the project repository interface. GetByID, Update and
// Delete answer ErrNotFound for an id no project has, a malformed one
// included; a list filtered by a malformed id lists nothing.
type Repository interface {
	Create(project *Project) error
	GetByID(id string) (*Project, error)
	GetAll() ([]*Project, error)
	// ListByOrg returns the projects in one org. It fails closed: an empty
	// orgID matches no rows (never every project), so a caller that could not
	// resolve an active workspace cannot leak cross-tenant projects.
	ListByOrg(orgID string) ([]*Project, error)
	Update(project *Project) error
	// Delete deletes the project and everything that belongs to it alone.
	Delete(id string) error
	// ListChildren returns the projects whose parent_project_id is id.
	ListChildren(id string) ([]*Project, error)
}
