# OpenV defined in OpenV

The requirements definition of the OpenV multi-agent development suite,
expressed as an OpenV project and loaded through the public API — a
dogfooding example and a realistic seed for demos.

Contents of `requirements.json`:

- **Product profile** — vision, problem statement, target users, the four
  standing constraints (BYO-AI personal-use compliance, no stored provider
  credentials, no background services, lean agent context) and success metrics
- **75 artifacts** — 4 personas, 10 stakeholder needs, 33 requirements in six
  sections (requirements core, guided definition & interviews, V&V,
  multi-tenancy & access control, agents & automation, execution & runners),
  8 design items, 10 test cases
- **69 typed links** — `derives-from` (requirement → need), `satisfies`
  (design item → requirement), `verifies` (test → requirement), `validates`
  (test → need)

## Load it

Against a running stack (`docker compose up -d`), with any registered account:

```powershell
cd examples\openv-multi-agent-suite
.\load-requirements.ps1 -Email you@example.com -Password '...' [-InviteAdminEmail colleague@example.com]
.\load-vv.ps1 -Email you@example.com -Password '...'
```

`load-requirements.ps1` creates (or reuses) a company workspace, the project,
profile, artifacts, links, and a v1.0 baseline. `load-vv.ps1` assigns a
verification method to every requirement and records a completed test run
with the evidence gathered during the suite build, leaving two honestly
uncovered requirements (Google sign-in and agent repo runs were never
live-tested) so the gap report shows real gaps.

The artifact `key` fields in the JSON are load-time handles only; OpenV
assigns its own ids.
