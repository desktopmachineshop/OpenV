package postgres

import (
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
)

// HostedWorkerRepository implements hostedworkers.Repository.
type HostedWorkerRepository struct {
	db *sql.DB
}

// NewHostedWorkerRepository creates a new hosted worker repository.
func NewHostedWorkerRepository(db *sql.DB) *HostedWorkerRepository {
	return &HostedWorkerRepository{db: db}
}

const hostedWorkerColumns = `id, org_id, container_name, worker_key_id, status, detail, created_by, created_at, updated_at`

func scanHostedWorker(row interface{ Scan(...interface{}) error }) (*hostedworkers.HostedWorker, error) {
	w := new(hostedworkers.HostedWorker)
	var workerKeyID, createdBy sql.NullString
	err := row.Scan(&w.ID, &w.OrgID, &w.ContainerName, &workerKeyID, &w.Status, &w.Detail, &createdBy, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if workerKeyID.Valid {
		v := workerKeyID.String
		w.WorkerKeyID = &v
	}
	if createdBy.Valid {
		v := createdBy.String
		w.CreatedBy = &v
	}
	return w, nil
}

// Save inserts a hosted worker.
func (r *HostedWorkerRepository) Save(w *hostedworkers.HostedWorker) error {
	_, err := r.db.Exec(`
		INSERT INTO hosted_workers (id, org_id, container_name, worker_key_id, status, detail, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, w.ID, w.OrgID, w.ContainerName, w.WorkerKeyID, w.Status, w.Detail, w.CreatedBy, w.CreatedAt, w.UpdatedAt)
	return err
}

// Update rewrites a hosted worker's mutable fields.
func (r *HostedWorkerRepository) Update(w *hostedworkers.HostedWorker) error {
	_, err := r.db.Exec(`
		UPDATE hosted_workers SET container_name = $2, worker_key_id = $3, status = $4, detail = $5, updated_at = $6
		WHERE id = $1
	`, w.ID, w.ContainerName, w.WorkerKeyID, w.Status, w.Detail, w.UpdatedAt)
	return err
}

// FindByOrg returns the org's hosted worker, or nil.
func (r *HostedWorkerRepository) FindByOrg(orgID string) (*hostedworkers.HostedWorker, error) {
	w, err := scanHostedWorker(r.db.QueryRow(`SELECT `+hostedWorkerColumns+` FROM hosted_workers WHERE org_id = $1`, orgID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return w, err
}

// FindByID returns a hosted worker, or nil.
func (r *HostedWorkerRepository) FindByID(id string) (*hostedworkers.HostedWorker, error) {
	w, err := scanHostedWorker(r.db.QueryRow(`SELECT `+hostedWorkerColumns+` FROM hosted_workers WHERE id = $1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return w, err
}

// Delete removes a hosted worker record.
func (r *HostedWorkerRepository) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM hosted_workers WHERE id = $1`, id)
	return err
}

// ListAll returns every hosted worker record.
func (r *HostedWorkerRepository) ListAll() ([]*hostedworkers.HostedWorker, error) {
	rows, err := r.db.Query(`SELECT ` + hostedWorkerColumns + ` FROM hosted_workers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*hostedworkers.HostedWorker
	for rows.Next() {
		w, err := scanHostedWorker(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}
