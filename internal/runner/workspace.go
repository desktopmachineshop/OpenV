package runner

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, text)
	}
	return text, nil
}

// PrepareWorkspace creates the run's working directory, checking out the
// project's repo when the agent has repo access. The returned workDir is the
// directory the agent should run in.
func PrepareWorkspace(baseDir string, run *agentruns.Run, agent *agents.Agent, conns []*repoconns.RepoConnection) (string, string, error) {
	workDir := filepath.Join(baseDir, run.ID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", "", err
	}

	if agent == nil || !agent.RepoAccess || len(conns) == 0 {
		return workDir, "", nil
	}

	conn := conns[0]
	branch := conn.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	repoDir := filepath.Join(workDir, "repo")
	shortID := run.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	agentBranch := "openv/agent-" + shortID

	switch {
	case conn.LocalPath != "":
		if _, err := runGit(conn.LocalPath, "worktree", "add", repoDir, "-b", agentBranch, branch); err != nil {
			// Fall back to a plain clone of the local repo.
			if _, cerr := runGit("", "clone", "--branch", branch, "--single-branch", conn.LocalPath, repoDir); cerr != nil {
				return "", "", fmt.Errorf("worktree add failed (%v) and clone failed: %w", err, cerr)
			}
			if _, berr := runGit(repoDir, "checkout", "-b", agentBranch); berr != nil {
				return "", "", berr
			}
			return repoDir, "cloned local repo " + conn.LocalPath + " on branch " + agentBranch, nil
		}
		return repoDir, "worktree of " + conn.LocalPath + " on branch " + agentBranch, nil
	case conn.RemoteURL != "":
		if _, err := runGit("", "clone", "--branch", branch, "--single-branch", conn.RemoteURL, repoDir); err != nil {
			return "", "", err
		}
		if _, err := runGit(repoDir, "checkout", "-b", agentBranch); err != nil {
			return "", "", err
		}
		return repoDir, "cloned " + conn.RemoteURL + " on branch " + agentBranch, nil
	}
	return workDir, "", nil
}

// FinishWorkspace commits any leftover changes in the run's repo (never
// pushes). Errors are log-only.
func FinishWorkspace(workDir string, repoUsed bool) {
	if !repoUsed {
		return
	}
	status, err := runGit(workDir, "status", "--porcelain")
	if err != nil {
		log.Printf("workspace %s: git status failed: %v", workDir, err)
		return
	}
	if status == "" {
		return
	}
	if _, err := runGit(workDir, "add", "-A"); err != nil {
		log.Printf("workspace %s: git add failed: %v", workDir, err)
		return
	}
	runID := filepath.Base(filepath.Dir(workDir))
	if _, err := runGit(workDir, "commit", "-m", "openv agent run "+runID); err != nil {
		log.Printf("workspace %s: git commit failed: %v", workDir, err)
	}
}

// CleanupOld removes run workspaces older than the given age. Errors are
// log-only.
func CleanupOld(baseDir string, olderThan time.Duration) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-olderThan)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		path := filepath.Join(baseDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			log.Printf("workspace cleanup: remove %s: %v", path, err)
		}
	}
}
