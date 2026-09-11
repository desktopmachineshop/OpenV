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

const sharedProductColumns = `p.id, p.category, p.name, p.description, p.vision, p.problem, p.target_users, p.created_at`

// voteWindow is the SQL expression for the rolling "this week" window. The
// database reckons it against its own clock, so the weekly leaderboard and
// the weekly count on a card can never disagree about when the week started.
const voteWindow = `NOW() - INTERVAL '7 days'`

// ListVisible returns unhidden products in the requested order, each carrying
// its all-time votes, its votes inside the weekly window, and whether the
// viewer has voted.
//
// One query does all three: the weekly count comes from a lateral subquery
// over shared_product_votes (it cannot be denormalised — it changes as time
// passes, not only as votes arrive), and the viewer's own vote from a left
// join that simply finds no row when the caller has no session user.
//
// The two "top" orderings drop entries with no votes: a leaderboard of
// products nobody voted for would be a second random list wearing a filter's
// name. Ties break on newest first and then on id, so the same five come back
// in the same order on every call.
func (r *SharedProductRepository) ListVisible(opts sharedproducts.ListOptions) ([]*sharedproducts.Product, error) {
	where := `p.hidden = FALSE`
	order := `p.created_at DESC, p.id DESC`
	switch opts.Sort {
	case sharedproducts.SortTop:
		where += ` AND p.votes > 0`
		order = `p.votes DESC, p.created_at DESC, p.id DESC`
	case sharedproducts.SortTopWeek:
		where += ` AND w.week_votes > 0`
		order = `w.week_votes DESC, p.created_at DESC, p.id DESC`
	}

	rows, err := r.db.Query(`
		SELECT `+sharedProductColumns+`, p.votes, w.week_votes, (v.user_id IS NOT NULL)
		FROM shared_products p
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS week_votes
			FROM shared_product_votes sv
			WHERE sv.product_id = p.id AND sv.created_at >= `+voteWindow+`
		) w ON TRUE
		LEFT JOIN shared_product_votes v ON v.product_id = p.id AND v.user_id = $2
		WHERE `+where+`
		ORDER BY `+order+`
		LIMIT $1
	`, opts.Limit, nullUUID(opts.ViewerID))
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
			&p.Votes, &p.VotesWeek, &p.Voted,
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

// AddVote records one person's vote for a visible entry and returns the
// number of distinct people who have now voted for it.
//
// Same shape as AddReport, and the same reason: the (product, user) primary
// key makes a second press a no-op, so a count reflects people rather than
// clicks. Hidden entries are refused — they are not in anyone's roll list, so
// a vote for one would be a number nobody can see being earned.
func (r *SharedProductRepository) AddVote(id, userID string) (int, error) {
	return r.changeVote(id, func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO shared_product_votes (product_id, user_id) VALUES ($1, $2)
			ON CONFLICT (product_id, user_id) DO NOTHING
		`, id, userID)
		return err
	})
}

// RemoveVote withdraws one person's vote and returns the new total.
// Withdrawing a vote that was never cast changes nothing.
func (r *SharedProductRepository) RemoveVote(id, userID string) (int, error) {
	return r.changeVote(id, func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`DELETE FROM shared_product_votes WHERE product_id = $1 AND user_id = $2`, id, userID,
		)
		return err
	})
}

// changeVote applies one vote change and recounts inside a single
// transaction, so the denormalised shared_products.votes column can never
// disagree with the rows it summarises.
func (r *SharedProductRepository) changeVote(id string, change func(*sql.Tx) error) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	var visible bool
	if err := tx.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM shared_products WHERE id = $1 AND hidden = FALSE)`, id,
	).Scan(&visible); err != nil {
		return 0, err
	}
	if !visible {
		return 0, sharedproducts.ErrNotFound
	}
	if err := change(tx); err != nil {
		return 0, err
	}

	var total int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM shared_product_votes WHERE product_id = $1`, id,
	).Scan(&total); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE shared_products SET votes = $2 WHERE id = $1`, id, total); err != nil {
		return 0, err
	}
	return total, tx.Commit()
}

// CountVotesWeek counts an entry's votes inside the rolling weekly window,
// against the database's clock — the same expression the weekly leaderboard
// orders by.
func (r *SharedProductRepository) CountVotesWeek(id string) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM shared_product_votes
		WHERE product_id = $1 AND created_at >= `+voteWindow,
		id,
	).Scan(&count)
	return count, err
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
