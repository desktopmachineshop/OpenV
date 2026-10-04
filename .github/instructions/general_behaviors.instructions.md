---
applyTo: "**"
---

# General Behaviors

- When code changes require a local rebuild or service restart, perform the rebuild/restart without asking the user. Avoid telling the user to rebuild unless they explicitly request it.
- Prefer taking action (local rebuilds, restarts) proactively after changes that affect running services. This never covers a deploy: promoting to release or redeploying a Railway service needs the maintainer's explicit request (`CLAUDE.md`, "Deployment").
- Put helper scripts you write for your own debugging in `temp_helpers/`, which `.gitignore` keeps out of the repository, and never reference them from the codebase.
