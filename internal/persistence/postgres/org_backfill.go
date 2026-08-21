package postgres

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var slugCleaner = regexp.MustCompile(`[^a-z0-9]+`)

// orgSlug builds a unique-ish slug from a display name plus an id fragment.
func orgSlug(name, id string) string {
	base := slugCleaner.ReplaceAllString(strings.ToLower(name), "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "org"
	}
	if len(base) > 40 {
		base = base[:40]
	}
	return base + "-" + strings.ReplaceAll(id, "-", "")[:8]
}

// BackfillOrgs migrates a pre-tenancy database: personal orgs for every
// user, org ownership for every existing row, and per-org agent file layout.
// Idempotent — safe on every boot; fresh databases fall through quickly.
// agentsDir is the root agents directory (flat legacy files are moved into
// per-org subdirectories).
func BackfillOrgs(db *sql.DB, agentsDir string) error {
	// 1. Personal org for every user without one.
	rows, err := db.Query(`
		SELECT u.id, u.name, u.email FROM users u
		WHERE NOT EXISTS (
			SELECT 1 FROM org_members m
			JOIN organizations o ON o.id = m.org_id AND o.org_type = 'personal'
			WHERE m.user_id = u.id
		)
		ORDER BY u.created_at
	`)
	if err != nil {
		return err
	}
	type pendingUser struct{ id, name, email string }
	var pending []pendingUser
	for rows.Next() {
		var p pendingUser
		if err := rows.Scan(&p.id, &p.name, &p.email); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range pending {
		display := p.name
		if display == "" {
			display = strings.SplitN(p.email, "@", 2)[0]
		}
		orgID := uuid.New().String()
		if _, err := db.Exec(`
			INSERT INTO organizations (id, name, slug, org_type, created_by)
			VALUES ($1, $2, $3, 'personal', $4)
		`, orgID, display+"'s Space", orgSlug(display, orgID), p.id); err != nil {
			return fmt.Errorf("backfill: personal org for %s: %w", p.email, err)
		}
		if _, err := db.Exec(`
			INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')
			ON CONFLICT DO NOTHING
		`, orgID, p.id); err != nil {
			return err
		}
		log.Printf("backfill: created personal org for %s", p.email)
	}

	// 2. Bootstrap org = earliest user's personal org. Fresh DB → done.
	var bootstrapOrg string
	err = db.QueryRow(`
		SELECT o.id FROM organizations o
		JOIN org_members m ON m.org_id = o.id
		JOIN users u ON u.id = m.user_id
		WHERE o.org_type = 'personal'
		ORDER BY u.created_at LIMIT 1
	`).Scan(&bootstrapOrg)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	// 3. Projects: earliest owner's personal org, else bootstrap.
	if _, err := db.Exec(`
		UPDATE projects p SET org_id = COALESCE((
			SELECT o.id FROM project_members pm
			JOIN org_members om ON om.user_id = pm.user_id
			JOIN organizations o ON o.id = om.org_id AND o.org_type = 'personal'
			WHERE pm.project_id = p.id AND pm.role = 'owner'
			ORDER BY pm.created_at LIMIT 1
		), $1)
		WHERE p.org_id IS NULL
	`, bootstrapOrg); err != nil {
		return fmt.Errorf("backfill: projects: %w", err)
	}

	// 4. Project-derivable tables; NULL-project rows fall to bootstrap.
	for _, table := range []string{"agent_teams", "automations", "agent_runs", "guided_sessions", "domain_events"} {
		if _, err := db.Exec(fmt.Sprintf(`
			UPDATE %s t SET org_id = p.org_id FROM projects p
			WHERE t.project_id = p.id AND t.org_id IS NULL
		`, table)); err != nil {
			return fmt.Errorf("backfill: %s via project: %w", table, err)
		}
		if _, err := db.Exec(fmt.Sprintf(`UPDATE %s SET org_id = $1 WHERE org_id IS NULL`, table), bootstrapOrg); err != nil {
			return fmt.Errorf("backfill: %s fallback: %w", table, err)
		}
	}

	// 5. Global tables → bootstrap org.
	for _, table := range []string{"agents", "provider_settings", "provider_logins"} {
		if _, err := db.Exec(fmt.Sprintf(`UPDATE %s SET org_id = $1 WHERE org_id IS NULL`, table), bootstrapOrg); err != nil {
			return fmt.Errorf("backfill: %s: %w", table, err)
		}
	}

	// 6. Move flat legacy agent files into the bootstrap org's directory and
	// fix registry paths.
	if agentsDir != "" {
		if entries, err := os.ReadDir(agentsDir); err == nil {
			orgDir := filepath.Join(agentsDir, bootstrapOrg)
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
					continue
				}
				if err := os.MkdirAll(orgDir, 0o755); err != nil {
					return err
				}
				src := filepath.Join(agentsDir, entry.Name())
				dst := filepath.Join(orgDir, entry.Name())
				if err := os.Rename(src, dst); err != nil {
					log.Printf("backfill: could not move agent file %s: %v", entry.Name(), err)
					continue
				}
				if _, err := db.Exec(`UPDATE agents SET file_path = $2 WHERE file_path = $1`, src, dst); err != nil {
					log.Printf("backfill: could not update agent file path for %s: %v", entry.Name(), err)
				}
			}
			// Legacy trash follows along.
			legacyTrash := filepath.Join(agentsDir, ".trash")
			if info, err := os.Stat(legacyTrash); err == nil && info.IsDir() {
				_ = os.Rename(legacyTrash, filepath.Join(agentsDir, bootstrapOrg, ".trash"))
			}
		}
	}

	// 7. NOT NULL promotions once everything is owned.
	return PromoteOrgColumns(db)
}
