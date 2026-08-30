OpenV Agent Connector
=====================

This program runs your personal OpenV runner on demand. It is not a
service: it only runs while its window is open.

The download is a single executable. On first run it unpacks its two
runner components (agentd and openv-mcp) into the same folder, so put it
somewhere permanent before running it.

Setup (Windows)
---------------
1. Move openv-connector.exe somewhere permanent, e.g. C:\OpenV\Connector
2. Double-click it once, or run:  openv-connector.exe install
   This registers the openv-connector:// link handler for your user account
   and unpacks the runner components next to the executable.
3. Back in OpenV, click "Pair connector". Your browser opens this app,
   which exchanges the one-time code for your personal runner key and starts
   the runner. (Codes expire after 10 minutes and work once.)

After pairing, the "Open connector" button in OpenV (or double-clicking
openv-connector.exe) starts your runner. Close the window to stop it.

Your AI subscription sign-ins (claude / codex / gemini CLIs) stay on this
machine — OpenV never sees them. The runner only picks up agent runs that
you launched.

Files (after first run)
-----------------------
openv-connector(.exe)  the launcher / protocol handler (the download)
agentd(.exe)           the runner daemon it starts (unpacked)
openv-mcp(.exe)        the tool bridge agents use to reach your workspace data (unpacked)
