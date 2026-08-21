# Phase 2: assign verification methods to every requirement and record the
# real verification evidence as a completed test run.
param(
  [string]$ApiBase = "http://localhost:8080",
  [Parameter(Mandatory)][string]$Email,
  [Parameter(Mandatory)][string]$Password,
  [string]$WorkspaceName = "OpenV",
  [string]$ProjectName = "OpenV Platform"
)
$ErrorActionPreference = "Stop"

$null = Invoke-WebRequest -UseBasicParsing -Method POST -Uri "$ApiBase/api/v1/auth/login" `
  -ContentType 'application/json' `
  -Body (@{ email = $Email; password = $Password } | ConvertTo-Json) -SessionVariable sess

function Invoke-Api {
  param([string]$Method, [string]$Path, $Body)
  $args = @{ Method = $Method; Uri = "$ApiBase$Path"; WebSession = $script:sess; ContentType = "application/json; charset=utf-8"; Headers = $script:hdr }
  if ($null -ne $Body) { $args.Body = [System.Text.Encoding]::UTF8.GetBytes(($Body | ConvertTo-Json -Depth 8)) }
  Invoke-RestMethod @args
}

$orgId = ((Invoke-RestMethod -Uri "$ApiBase/api/v1/orgs" -WebSession $sess).orgs | Where-Object { $_.name -eq $WorkspaceName }).id
$hdr = @{ 'X-Org-ID' = $orgId }
$projectId = (Invoke-Api GET "/api/v1/projects" | Where-Object { $_.name -eq $ProjectName }).id
Write-Host "project: $projectId"

$artifacts = Invoke-Api GET "/api/v1/artifacts?project_id=$projectId"
$byTitle = @{}
foreach ($a in $artifacts) { $byTitle[$a.title] = $a }
Write-Host "artifacts fetched: $($artifacts.Count)"

# title -> method, status ("" = honestly not yet verified)
$methods = @{
  "Hierarchical versioned modules"             = @("demonstration", "verified")
  "Typed artifacts with attributes"            = @("demonstration", "verified")
  "Semantically constrained links"             = @("test", "")
  "Artifact version history"                   = @("demonstration", "verified")
  "Project baselines"                          = @("demonstration", "verified")
  "Export and reporting"                       = @("demonstration", "verified")
  "Guided product definition wizard"           = @("demonstration", "verified")
  "Interview invitation links"                 = @("test", "")
  "Conversational AI elicitation"              = @("test", "")
  "Coverage computation"                       = @("test", "")
  "Traceability matrix"                        = @("test", "")
  "Gap reporting"                              = @("test", "")
  "Test run recording"                         = @("demonstration", "verified")
  "Personal spaces"                            = @("demonstration", "verified")
  "Company workspaces"                         = @("test", "")
  "Per-project access grants"                  = @("test", "")
  "Tenant isolation"                           = @("test", "")
  "Authentication"                             = @("demonstration", "")
  "GUI-editable agent definitions"             = @("demonstration", "verified")
  "Lean agent context"                         = @("analysis", "verified")
  "Proposal-gated writes"                      = @("test", "")
  "User-composable crews"                      = @("demonstration", "verified")
  "Unified kanban"                             = @("demonstration", "verified")
  "Manual, scheduled and triggered automation" = @("analysis", "verified")
  "Code repository operation"                  = @("demonstration", "")
  "Bring-your-own-AI personal runners"         = @("demonstration", "verified")
  "Personal runner scoping"                    = @("test", "")
  "First-refusal routing"                      = @("test", "")
  "Token-only hosted runners"                  = @("test", "")
  "Hosted repo exclusion"                      = @("test", "")
  "Hashed credentials, shown once"             = @("test", "")
  "On-demand Agent Connector"                  = @("test", "")
  "Credential-free pairing links"              = @("test", "")
}

$updated = 0
foreach ($title in $methods.Keys) {
  $a = $byTitle[$title]
  if (-not $a) { Write-Host "MISSING: $title"; continue }
  $attrs = @{}
  if ($a.attributes) { $a.attributes.PSObject.Properties | ForEach-Object { $attrs[$_.Name] = $_.Value } }
  $attrs["verification_method"] = $methods[$title][0]
  if ($methods[$title][1] -ne "") { $attrs["verification_status"] = $methods[$title][1] }
  $body = @{ type = $a.type; title = $a.title; body = $a.body; attributes = $attrs }
  if ($a.parent_id) { $body.parent_id = $a.parent_id }
  if ($null -ne $a.sort_order) { $body.sort_order = $a.sort_order }
  $null = Invoke-Api PUT "/api/v1/artifacts/$($a.id)" $body
  $updated++
}
Write-Host "requirements updated: $updated"

# Test run recording the evidence that already exists (Go suites + live E2E).
$run = Invoke-Api POST "/api/v1/projects/$projectId/test-runs" @{
  name        = "Platform verification evidence 2026-08-21"
  description = "Automated Go test suites (authz matrix, claim routing, key rotation, link rules) plus live end-to-end checks performed during the phase 1-3 and Agent Connector builds."
}
Write-Host "test run: $($run.id)"

$evidence = @{
  "Authorization matrix suite"      = "go test internal/api authz matrix suite green"
  "Tenant isolation end-to-end"     = "Live two-account check: all cross-org access denied; team-grant escalation correct"
  "Claim routing suite"             = "Go claim-routing suite green; live grace-window reservation and hosted takeover confirmed"
  "Personal key rotation suite"     = "Go workerkeys suite green: rotation revokes prior key"
  "Hosted token-mode smoke test"    = "openv-worker image booted with API key: logged_in=true API key mode; repo runs excluded"
  "Connector pairing end-to-end"    = "Live on Windows host: deep-link pair, single-use + expiry enforced, agentd online, clean uninstall"
  "Agent proposal loop end-to-end"  = "Live run: claim -> run token -> proposal -> approve -> artifact materialized"
  "Link rule validation suite"      = "Go link validation suite green"
  "V&V dashboard verification"      = "Coverage, matrix and gaps verified against known project state after snake_case fix"
  "Interview elicitation flow"      = "Live: external link chat produced interview-tagged draft artifacts"
}

$results = 0
foreach ($title in $evidence.Keys) {
  $tc = $byTitle[$title]
  if (-not $tc) { Write-Host "MISSING TC: $title"; continue }
  $null = Invoke-Api POST "/api/v1/test-runs/$($run.id)/results" @{
    test_case_id = $tc.id
    status       = "pass"
    notes        = $evidence[$title]
    evidence     = @()
  }
  $results++
}
Write-Host "results recorded: $results"

$null = Invoke-Api PUT "/api/v1/test-runs/$($run.id)" @{ status = "completed" }
Write-Host "run completed"

$coverage = Invoke-Api GET "/api/v1/projects/$projectId/vv/coverage"
Write-Host ("summary: " + ($coverage.summary | ConvertTo-Json -Compress))
