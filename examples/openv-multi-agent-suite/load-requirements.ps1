# Loads the OpenV multi-agent suite requirements definition into a fresh
# OpenV project via the public API. PowerShell 5.1 compatible.
#
#   .\load-requirements.ps1 -Email you@example.com -Password ... [-InviteAdminEmail colleague@example.com]
param(
  [string]$ApiBase = "http://localhost:8080",
  [string]$DefPath = "$PSScriptRoot\requirements.json",
  [Parameter(Mandatory)][string]$Email,
  [Parameter(Mandatory)][string]$Password,
  [string]$WorkspaceName = "OpenV",
  [string]$ProjectName = "OpenV Platform",
  [string]$InviteAdminEmail = ""
)

$ErrorActionPreference = "Stop"
$def = Get-Content $DefPath -Raw -Encoding UTF8 | ConvertFrom-Json

function Invoke-Api {
  param([string]$Method, [string]$Path, $Body, [hashtable]$Headers)
  $args = @{
    Method      = $Method
    Uri         = "$ApiBase$Path"
    WebSession  = $script:sess
    ContentType = "application/json; charset=utf-8"
  }
  if ($Headers) { $args.Headers = $Headers }
  if ($null -ne $Body) {
    $json = $Body | ConvertTo-Json -Depth 8
    $args.Body = [System.Text.Encoding]::UTF8.GetBytes($json)
  }
  Invoke-RestMethod @args
}

# 1. Sign in.
$null = Invoke-WebRequest -UseBasicParsing -Method POST -Uri "$ApiBase/api/v1/auth/login" `
  -ContentType 'application/json' `
  -Body (@{ email = $Email; password = $Password } | ConvertTo-Json) `
  -SessionVariable sess
Write-Host "signed in"

# 2. Create (or reuse) the company workspace.
$orgsResp = Invoke-Api GET "/api/v1/orgs"
$org = $orgsResp.orgs | Where-Object { $_.name -eq $WorkspaceName } | Select-Object -First 1
if (-not $org) {
  $null = Invoke-Api POST "/api/v1/orgs" @{ name = $WorkspaceName }
  $orgsResp = Invoke-Api GET "/api/v1/orgs"
  $org = $orgsResp.orgs | Where-Object { $_.name -eq $WorkspaceName } | Select-Object -First 1
}
$orgId = $org.id
$hdr = @{ 'X-Org-ID' = $orgId }
Write-Host "workspace ${WorkspaceName}: $orgId"

# 3. Optionally invite an existing user as workspace admin.
if ($InviteAdminEmail) {
  try {
    $null = Invoke-Api POST "/api/v1/orgs/$orgId/members" @{ email = $InviteAdminEmail; role = "admin" } $hdr
    Write-Host "added $InviteAdminEmail as admin"
  } catch { Write-Host "admin invite: $($_.Exception.Message)" }
}

# 4. Create the project.
$project = Invoke-Api POST "/api/v1/projects" @{
  name        = $ProjectName
  description = "The requirements definition of OpenV itself: multi-agent development suite, multi-tenant SaaS, predictable BYO-AI execution."
} $hdr
$projectId = $project.id
Write-Host "project: $projectId"

# 5. Product profile.
$null = Invoke-Api PUT "/api/v1/projects/$projectId/profile" $def.profile $hdr
Write-Host "profile saved"

# 6. Artifacts (in file order; parents are declared before children).
$ids = @{}
$i = 0
$artifactCount = 0
foreach ($a in $def.artifacts) {
  $i += 10
  $body = @{
    project_id = $projectId
    type       = $a.type
    title      = $a.title
    body       = $a.body
    sort_order = $i
  }
  if ($a.parent) {
    if (-not $ids.ContainsKey($a.parent)) { throw "parent $($a.parent) not created yet (artifact $($a.key))" }
    $body.parent_id = $ids[$a.parent]
  }
  if ($a.attributes) {
    $attrs = @{}
    $a.attributes.PSObject.Properties | ForEach-Object { $attrs[$_.Name] = $_.Value }
    $body.attributes = $attrs
  }
  $created = Invoke-Api POST "/api/v1/artifacts" $body $hdr
  $ids[$a.key] = $created.id
  $artifactCount++
}
Write-Host "artifacts created: $artifactCount"

# 7. Links.
$linkCount = 0
foreach ($l in $def.links) {
  if (-not $ids.ContainsKey($l.from)) { throw "unknown link source $($l.from)" }
  if (-not $ids.ContainsKey($l.to)) { throw "unknown link target $($l.to)" }
  $null = Invoke-Api POST "/api/v1/links" @{
    from_id = $ids[$l.from]
    to_id   = $ids[$l.to]
    type    = $l.type
  } $hdr
  $linkCount++
}
Write-Host "links created: $linkCount"

# 8. Baseline.
$null = Invoke-Api POST "/api/v1/projects/$projectId/baselines" @{
  name = "v1.0 - Multi-agent suite definition"
} $hdr
Write-Host "baseline captured"

Write-Host "DONE project=$projectId org=$orgId"
Write-Host "Next: .\load-vv.ps1 -Email $Email -Password ... to assign verification methods and record evidence."
