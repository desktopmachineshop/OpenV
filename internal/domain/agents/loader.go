package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// ParseFile splits a markdown agent file into frontmatter + body.
// Format: leading "---\n<yaml>\n---\n<system prompt body>".
func ParseFile(content string) (*Definition, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return nil, errors.New("agent file must start with '---' YAML frontmatter")
	}
	rest := normalized[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, errors.New("agent file frontmatter is not closed with '---'")
	}
	front := rest[:end]
	body := rest[end+4:]
	body = strings.TrimPrefix(body, "\n")

	def := &Definition{}
	if err := yaml.Unmarshal([]byte(front), def); err != nil {
		return nil, fmt.Errorf("invalid agent frontmatter: %w", err)
	}
	def.SystemPrompt = strings.TrimSpace(body)
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return def, nil
}

// SerializeFile renders a definition back to markdown file content.
func SerializeFile(def *Definition) (string, error) {
	if err := def.Validate(); err != nil {
		return "", err
	}
	front, err := yaml.Marshal(def)
	if err != nil {
		return "", err
	}
	return "---\n" + string(front) + "---\n" + def.SystemPrompt + "\n", nil
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// FileService implements Service over a directory of markdown files plus
// the registry repository.
type FileService struct {
	dir  string
	repo Repository
}

// NewFileService creates the agent service rooted at dir (created if missing).
func NewFileService(dir string, repo Repository) (*FileService, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create agents directory %s: %w", dir, err)
	}
	return &FileService{dir: dir, repo: repo}, nil
}

func (s *FileService) orgDir(orgID string) string {
	return filepath.Join(s.dir, orgID)
}

func (s *FileService) filePath(orgID, slug string) string {
	return filepath.Join(s.orgDir(orgID), slug+".md")
}

// List returns all registered agents in an org.
func (s *FileService) List(orgID string) ([]*Agent, error) { return s.repo.List(orgID) }

// Get returns an agent by id.
func (s *FileService) Get(id string) (*Agent, error) { return s.repo.FindByID(id) }

// GetBySlug returns an agent by slug within an org.
func (s *FileService) GetBySlug(orgID, slug string) (*Agent, error) {
	return s.repo.FindBySlug(orgID, slug)
}

// SaveDefinition writes the definition to its markdown file and syncs the registry.
func (s *FileService) SaveDefinition(orgID string, def *Definition) (*Agent, error) {
	if orgID == "" {
		return nil, errors.New("organization id is required")
	}
	content, err := SerializeFile(def)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.orgDir(orgID), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create org agents directory: %w", err)
	}
	path := s.filePath(orgID, def.Slug)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write agent file: %w", err)
	}
	return s.syncOne(orgID, def, path, content)
}

// RawFile returns the markdown content for a slug.
func (s *FileService) RawFile(orgID, slug string) (string, error) {
	data, err := os.ReadFile(s.filePath(orgID, slug))
	if err != nil {
		return "", fmt.Errorf("failed to read agent file for %s: %w", slug, err)
	}
	return string(data), nil
}

// SaveRawFile validates and writes raw markdown content for a slug.
func (s *FileService) SaveRawFile(orgID, slug string, content string) (*Agent, error) {
	if orgID == "" {
		return nil, errors.New("organization id is required")
	}
	def, err := ParseFile(content)
	if err != nil {
		return nil, err
	}
	if def.Slug != slug {
		return nil, fmt.Errorf("frontmatter slug %q does not match agent %q", def.Slug, slug)
	}
	if err := os.MkdirAll(s.orgDir(orgID), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create org agents directory: %w", err)
	}
	path := s.filePath(orgID, slug)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write agent file: %w", err)
	}
	return s.syncOne(orgID, def, path, content)
}

// Delete moves the agent's file to the org's .trash subfolder and removes
// the registry row.
func (s *FileService) Delete(orgID, slug string) error {
	agent, err := s.repo.FindBySlug(orgID, slug)
	if err != nil {
		return err
	}
	if agent == nil {
		return ErrNotFound
	}
	trashDir := filepath.Join(s.orgDir(orgID), ".trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		return err
	}
	src := s.filePath(orgID, slug)
	if _, err := os.Stat(src); err == nil {
		dst := filepath.Join(trashDir, fmt.Sprintf("%s-%d.md", slug, time.Now().Unix()))
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("failed to trash agent file: %w", err)
		}
	}
	return s.repo.Delete(agent.ID)
}

// SyncFromDisk reconciles every *.md in one org's directory with the
// registry. Files that fail to parse are skipped with an error aggregated
// in the result. A missing org directory is not an error (nothing to sync).
func (s *FileService) SyncFromDisk(orgID string) error {
	if orgID == "" {
		return errors.New("organization id is required")
	}
	entries, err := os.ReadDir(s.orgDir(orgID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var errs []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(s.orgDir(orgID), entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", entry.Name(), err))
			continue
		}
		def, err := ParseFile(string(data))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", entry.Name(), err))
			continue
		}
		if _, err := s.syncOne(orgID, def, path, string(data)); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", entry.Name(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("agent sync completed with errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// SyncAllFromDisk walks the org subdirectories at the root and syncs each
// one. Non-directory entries (legacy flat files) and dot-directories are
// skipped.
func (s *FileService) SyncAllFromDisk() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var errs []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if err := s.SyncFromDisk(entry.Name()); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", entry.Name(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("agent sync completed with errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *FileService) syncOne(orgID string, def *Definition, path, content string) (*Agent, error) {
	existing, err := s.repo.FindBySlug(orgID, def.Slug)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	hash := contentHash(content)
	if existing != nil {
		existing.Name = def.Name
		existing.Description = def.Description
		existing.Provider = def.Provider
		existing.Model = def.Model
		existing.Effort = def.Effort
		existing.AllowedTools = def.AllowedTools
		existing.WriteMode = def.WriteMode
		existing.RepoAccess = def.RepoAccess
		existing.MaxTurns = def.MaxTurns
		existing.TimeoutSeconds = def.TimeoutSeconds
		existing.Config = def.Config
		existing.SystemPrompt = def.SystemPrompt
		existing.FilePath = path
		existing.ContentHash = hash
		existing.SyncedAt = &now
		existing.UpdatedAt = now
		if err := s.repo.Update(existing); err != nil {
			return nil, err
		}
		return existing, nil
	}

	agent := &Agent{
		ID:             uuid.New().String(),
		OrgID:          orgID,
		Slug:           def.Slug,
		Name:           def.Name,
		Description:    def.Description,
		Provider:       def.Provider,
		Model:          def.Model,
		Effort:         def.Effort,
		AllowedTools:   def.AllowedTools,
		WriteMode:      def.WriteMode,
		RepoAccess:     def.RepoAccess,
		MaxTurns:       def.MaxTurns,
		TimeoutSeconds: def.TimeoutSeconds,
		Config:         def.Config,
		SystemPrompt:   def.SystemPrompt,
		FilePath:       path,
		ContentHash:    hash,
		SyncedAt:       &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.Save(agent); err != nil {
		return nil, err
	}
	return agent, nil
}
