package runner

import (
	"context"
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
	return runGitCtx(context.Background(), dir, args...)
}

func runGitCtx(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		// Surface the cancellation itself so callers can errors.Is on it.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return text, fmt.Errorf("git %s: %w", strings.Join(args, " "), ctxErr)
		}
		return text, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, text)
	}
	return text, nil
}

// PrepareWorkspace creates the run's working directory, checking out the
// project's repo when the agent has repo access. The returned workDir is the
// directory the agent should run in. Cancelling ctx aborts any in-flight git
// operation (a clone of a large repo can run for minutes); the run's
// cancel-during-prep path in worker.go relies on this.
func PrepareWorkspace(ctx context.Context, baseDir string, run *agentruns.Run, agent *agents.Agent, conns []*repoconns.RepoConnection) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
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
	case conn.MyLocalPath != "":
		if _, err := runGitCtx(ctx, conn.MyLocalPath, "worktree", "add", repoDir, "-b", agentBranch, branch); err != nil {
			if ctx.Err() != nil {
				return "", "", err
			}
			// Fall back to a plain clone of the local repo.
			if _, cerr := runGitCtx(ctx, "", "clone", "--branch", branch, "--single-branch", conn.MyLocalPath, repoDir); cerr != nil {
				return "", "", fmt.Errorf("worktree add failed (%v) and clone failed: %w", err, cerr)
			}
			if _, berr := runGitCtx(ctx, repoDir, "checkout", "-b", agentBranch); berr != nil {
				return "", "", berr
			}
			return repoDir, "cloned local repo " + conn.MyLocalPath + " on branch " + agentBranch, nil
		}
		return repoDir, "worktree of " + conn.MyLocalPath + " on branch " + agentBranch, nil
	case conn.RemoteURL != "":
		if _, err := runGitCtx(ctx, "", "clone", "--branch", branch, "--single-branch", conn.RemoteURL, repoDir); err != nil {
			return "", "", err
		}
		if _, err := runGitCtx(ctx, repoDir, "checkout", "-b", agentBranch); err != nil {
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
