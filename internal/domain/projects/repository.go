package projects

// Repository defines the project repository interface
type Repository interface {
	Create(project *Project) error
	GetByID(id string) (*Project, error)
	GetAll() ([]*Project, error)
	Update(project *Project) error
	Delete(id string) error
}
