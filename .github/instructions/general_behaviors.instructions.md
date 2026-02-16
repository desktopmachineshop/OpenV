# General Behaviors

- When code changes require a rebuild or service restart, perform the rebuild/restart without asking the user. Avoid telling the user to rebuild unless they explicitly request it.
- Prefer taking action (rebuilds, restarts) proactively after changes that affect running services.
- Developing on a Windows machine and the terminal is PowerShell, use PowerShell syntax for environment variables and commands.
- When creating helper scripts etc. for use by AI model for debugging etc. place them inside temp_helpers/ directory and do not reference them in the main codebase. These are for temporary use by the AI and should not be part of the permanent codebase. This folder and contents should not be committed to the repository.