package postgres

import (
	"database/sql"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
)

// SharedProductRepository implements sharedproducts.Repository over postgres.
//
// This is the one repository with no org scoping in its reads: the pool is
// shared by every workspace by design. created_by_org is written for rate
// limiting and takedown and is never a filter on what a caller may see.
type SharedProductRepository struct {
	db *sql.DB
}

// NewSharedProductRepository creates a new repository.
func NewSharedProductRepository(db *sql.DB) *SharedProductRepository {
	return &SharedProductRepository{db: db}
}

const sharedProductColumns = `id, category, name, description, vision, problem, target_users, created_at`

// ListVisible returns unhidden products, newest first.
func (r *SharedProductRepository) ListVisible(limit int) ([]*sharedproducts.Product, error) {
	rows, err := r.db.Query(`
		SELECT `+sharedProductColumns+`
		FROM shared_products
		WHERE hidden = FALSE
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	products := []*sharedproducts.Product{}
	for rows.Next() {
		p := &sharedproducts.Product{}
		if err := rows.Scan(
			&p.ID, &p.Category, &p.Name, &p.Description,
			&p.Vision, &p.Problem, &p.TargetUsers, &p.CreatedAt,
		); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

// Create inserts one product. A repeat of an existing name (normalized)
// surfaces as sharedproducts.ErrDuplicate rather than a raw driver error.
func (r *SharedProductRepository) Create(p *sharedproducts.Product) error {
	_, err := r.db.Exec(`
		INSERT INTO shared_products (
			id, name_key, category, name, description, vision, problem,
			target_users, created_by_org, created_by_user, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		p.ID, p.NameKey, p.Category, p.Name, p.Description, p.Vision,
		p.Problem, p.TargetUsers, nullUUID(p.CreatedByOrg), nullUUID(p.CreatedByUser), p.CreatedAt,
	)
	if err != nil && isUniqueViolation(err) {
		return sharedproducts.ErrDuplicate
	}
	return err
}

// CountByOrgSince counts an org's publications inside the rate-limit window.
// Hidden rows count: publishing something that got taken down does not buy
// the workspace a fresh allowance.
func (r *SharedProductRepository) CountByOrgSince(orgID string, since time.Time) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM shared_products
		WHERE created_by_org = $1 AND created_at >= $2
	`, nullUUID(orgID), since).Scan(&count)
	return count, err
}

// CountVisible counts the unhidden pool, for the global ceiling.
func (r *SharedProductRepository) CountVisible() (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM shared_products WHERE hidden = FALSE`).Scan(&count)
	return count, err
}

// AddReport records one person's report and returns the number of distinct
// people who have now reported the entry. Reporting twice is a no-op, so no
// single account can push an entry over the auto-hide threshold alone.
func (r *SharedProductRepository) AddReport(id, userID string) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM shared_products WHERE id = $1)`, id).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, sharedproducts.ErrNotFound
	}
	if _, err := tx.Exec(`
		INSERT INTO shared_product_reports (product_id, user_id) VALUES ($1, $2)
		ON CONFLICT (product_id, user_id) DO NOTHING
	`, id, userID); err != nil {
		return 0, err
	}

	var total int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM shared_product_reports WHERE product_id = $1`, id,
	).Scan(&total); err != nil {
		return 0, err
	}
	// Mirror the distinct count onto the product for admin triage.
	if _, err := tx.Exec(`UPDATE shared_products SET reports = $2 WHERE id = $1`, id, total); err != nil {
		return 0, err
	}
	return total, tx.Commit()
}

// SetHidden hides or unhides an entry.
func (r *SharedProductRepository) SetHidden(id string, hidden bool) error {
	res, err := r.db.Exec(`UPDATE shared_products SET hidden = $2 WHERE id = $1`, id, hidden)
	if err != nil {
		return err
	}
	return requireOneRow(res)
}

// Delete removes an entry outright.
func (r *SharedProductRepository) Delete(id string) error {
	res, err := r.db.Exec(`DELETE FROM shared_products WHERE id = $1`, id)
	if err != nil {
		return err
	}
	return requireOneRow(res)
}

// requireOneRow maps "nothing matched" onto ErrNotFound.
func requireOneRow(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sharedproducts.ErrNotFound
	}
	return nil
}

// nullUUID keeps an empty id out of a UUID column.
func nullUUID(id string) interface{} {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return id
}

// isUniqueViolation reports whether err is a postgres unique-constraint
// failure (SQLSTATE 23505), without importing the driver's error type.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value")
}
