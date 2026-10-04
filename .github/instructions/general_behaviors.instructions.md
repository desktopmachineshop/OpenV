---
applyTo: "**"
---

# General Behaviors

- When code changes require a rebuild or service restart, perform the rebuild/restart without asking the user. Avoid telling the user to rebuild unless they explicitly request it.
- Prefer taking action (rebuilds, restarts) proactively after changes that affect running services.
- Put helper scripts you write for your own debugging in `temp_helpers/`, which `.gitignore` keeps out of the repository, and never reference them from the codebase.
