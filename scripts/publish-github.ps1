# Publish this repository to GitHub using the REST API (no local git required).
#
# Why this script exists: git is not installed on this machine, and the GitHub
# Git Data API lets us build blobs / a tree / a commit and update a ref directly,
# which is enough for a normal first push.
#
# NOTE: this file is intentionally pure ASCII. Windows PowerShell 5.1 decodes
# .ps1 files as ANSI when there is no BOM, which would corrupt non-ASCII text
# and break parsing. Keeping it ASCII makes it safe in every host.
#
# Usage:
#   $env:GITHUB_TOKEN = "ghp_xxx"
#   powershell -ExecutionPolicy Bypass -File .\scripts\publish-github.ps1 -RepoName lucky-draw
#
# Parameters:
#   -RepoName         repository name (default: lucky-draw)
#   -Description      repository description
#   -Private          create a private repository (default: public)
#   -Branch           target branch (default: main)
#   -CommitMessage    first commit message
#   -CommitAuthorName author name for the commit
#   -CommitAuthorEmail author email (defaults to GitHub noreply address)
#   -DryRun           only list the files that would be uploaded

[CmdletBinding()]
param(
    [string]$RepoName = 'lucky-draw',
    [string]$Description = 'Weighted random number generator for game prize draws: weighted sampling + whole-round constraint + crypto/rand + built-in web console',
    [switch]$Private,
    [string]$Branch = 'main',
    [string]$CommitMessage = 'feat: initial commit of lucky-draw lottery service',
    [string]$CommitAuthorName = 'lucky-draw',
    [string]$CommitAuthorEmail = '',
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$ApiBase = 'https://api.github.com'
$RepoRoot = Split-Path -Parent $PSScriptRoot

# Explicit allow-list instead of walking the directory, so build artifacts and
# secrets can never be uploaded by accident.
$IncludeFiles = @(
    '.dockerignore',
    '.env.example',
    '.gitignore',
    'CHANGELOG.md',
    'Dockerfile',
    'LICENSE',
    'Makefile',
    'README.md',
    'api/openapi.yaml',
    'cmd/draw/main.go',
    'docker-compose.yml',
    'docs/api-examples.md',
    'go.mod',
    'internal/config/config.go',
    'internal/config/config_test.go',
    'internal/engine/engine.go',
    'internal/engine/engine_test.go',
    'internal/engine/probability_test.go',
    'internal/httpapi/assets.go',
    'internal/httpapi/dto.go',
    'internal/httpapi/server.go',
    'internal/httpapi/server_test.go',
    'internal/httpapi/web/app.js',
    'internal/httpapi/web/index.html',
    'internal/randsource/randsource.go',
    'internal/scenario/scenario.go',
    'internal/scenario/scenario_test.go',
    'main.go',
    'scripts/publish-github.ps1'
)

# Second line of defense against ever committing these.
$ForbiddenPatterns = @('*.exe', '*.log', '.env', '*.pem', '*.key', 'secrets/*', '.build/*', 'build/*', 'dist/*')

function Write-Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Write-Ok($msg) { Write-Host "    $msg" -ForegroundColor Green }
function Write-Note($msg) { Write-Host "    $msg" -ForegroundColor Yellow }

# ---------------------------------------------------------------------------
# 1. Validate token and collect files
# ---------------------------------------------------------------------------

$token = $env:GITHUB_TOKEN
if (-not $token) { $token = $env:GH_TOKEN }
if (-not $token -and -not $DryRun) {
    throw "Missing token. Set it first: `$env:GITHUB_TOKEN = 'ghp_xxx'"
}

Write-Step "Collecting files (repo root: $RepoRoot)"
$files = @()
$totalBytes = 0

foreach ($rel in $IncludeFiles) {
    $full = Join-Path $RepoRoot ($rel -replace '/', [IO.Path]::DirectorySeparatorChar)
    if (-not (Test-Path -LiteralPath $full -PathType Leaf)) {
        throw "File listed but not found: $rel ($full)"
    }
    foreach ($pat in $ForbiddenPatterns) {
        if ($rel -like $pat) { throw "Refusing to commit forbidden file: $rel" }
    }
    $bytes = [IO.File]::ReadAllBytes($full)
    $totalBytes += $bytes.Length
    $files += [pscustomobject]@{
        Path = $rel
        Bytes = $bytes
        Size = $bytes.Length
    }
}

Write-Ok ("{0} files, {1:N1} KB total" -f $files.Count, ($totalBytes / 1KB))
foreach ($f in $files) { Write-Host ("      {0,10:N0} B  {1}" -f $f.Size, $f.Path) }

if ($DryRun) {
    Write-Note 'DryRun: no network requests were made.'
    return
}

# ---------------------------------------------------------------------------
# 2. Verify identity
# ---------------------------------------------------------------------------

$Headers = @{
    Authorization = "Bearer $token"
    Accept = 'application/vnd.github+json'
    'X-GitHub-Api-Version' = '2022-11-28'
    'User-Agent' = 'lucky-draw-publisher'
}

Write-Step 'Verifying token and account'
$me = Invoke-RestMethod -Uri "$ApiBase/user" -Headers $Headers -Method Get
$owner = $me.login
Write-Ok "Authenticated as $owner ($($me.html_url))"

if (-not $CommitAuthorEmail) {
    # Use GitHub's noreply address so no private email ends up in the history.
    $CommitAuthorEmail = "$($me.id)+$owner@users.noreply.github.com"
}

# ---------------------------------------------------------------------------
# 3. Create the repository (reuse it when it already exists)
# ---------------------------------------------------------------------------

Write-Step "Creating repository $owner/$RepoName"
$repoPayload = @{
    name = $RepoName
    description = $Description
    private = [bool]$Private
    has_issues = $true
    has_wiki = $false
    auto_init = $false
} | ConvertTo-Json

try {
    $repo = Invoke-RestMethod -Uri "$ApiBase/user/repos" -Headers $Headers -Method Post `
        -Body $repoPayload -ContentType 'application/json; charset=utf-8'
    Write-Ok "Repository created: $($repo.html_url)"
} catch {
    $status = $_.Exception.Response.StatusCode.value__
    if ($status -eq 422) {
        $repo = Invoke-RestMethod -Uri "$ApiBase/repos/$owner/$RepoName" -Headers $Headers -Method Get
        Write-Note "Repository already exists, pushing into it: $($repo.html_url)"
    } else {
        throw "Failed to create repository (HTTP $status): $($_.ErrorDetails.Message)"
    }
}

$defaultBranch = $repo.default_branch
Write-Ok "Default branch: $defaultBranch"

# ---------------------------------------------------------------------------
# 4. Upload file contents as blobs
# ---------------------------------------------------------------------------

Write-Step 'Uploading file contents (blobs)'
$treeItems = @()
foreach ($f in $files) {
    $b64 = [Convert]::ToBase64String($f.Bytes)
    $body = @{ content = $b64; encoding = 'base64' } | ConvertTo-Json -Compress
    $blob = Invoke-RestMethod -Uri "$ApiBase/repos/$owner/$RepoName/git/blobs" -Headers $Headers `
        -Method Post -Body $body -ContentType 'application/json; charset=utf-8'
    $treeItems += @{
        path = $f.Path
        mode = '100644'
        type = 'blob'
        sha = $blob.sha
    }
    Write-Host ("      ok  {0}" -f $f.Path)
}

# ---------------------------------------------------------------------------
# 5. Create tree + commit, then update the branch ref
# ---------------------------------------------------------------------------

Write-Step 'Creating tree'
$treeBody = @{ tree = $treeItems } | ConvertTo-Json -Depth 6 -Compress
$newTree = Invoke-RestMethod -Uri "$ApiBase/repos/$owner/$RepoName/git/trees" -Headers $Headers `
    -Method Post -Body $treeBody -ContentType 'application/json; charset=utf-8'
Write-Ok "tree sha = $($newTree.sha)"

# When reusing an existing repository the target branch may already have a HEAD,
# which must be recorded as the parent commit.
$parentSha = $null
try {
    $ref = Invoke-RestMethod -Uri "$ApiBase/repos/$owner/$RepoName/git/ref/heads/$Branch" -Headers $Headers -Method Get
    $parentSha = $ref.object.sha
    Write-Ok "Branch $Branch exists, parent commit = $($parentSha.Substring(0, 10))"
} catch {
    Write-Ok "Branch $Branch does not exist yet; this will be the root commit"
}

Write-Step 'Creating commit'
$parents = @()
if ($parentSha) { $parents = @($parentSha) }
$commitBody = @{
    message = $CommitMessage
    tree = $newTree.sha
    author = @{ name = $CommitAuthorName; email = $CommitAuthorEmail }
    parents = $parents
} | ConvertTo-Json -Depth 5 -Compress

$commit = Invoke-RestMethod -Uri "$ApiBase/repos/$owner/$RepoName/git/commits" -Headers $Headers `
    -Method Post -Body $commitBody -ContentType 'application/json; charset=utf-8'
Write-Ok "commit sha = $($commit.sha)"

Write-Step "Updating branch ref $Branch"
if ($parentSha) {
    $refBody = @{ sha = $commit.sha; force = $false } | ConvertTo-Json -Compress
    $null = Invoke-RestMethod -Uri "$ApiBase/repos/$owner/$RepoName/git/refs/heads/$Branch" -Headers $Headers `
        -Method Patch -Body $refBody -ContentType 'application/json; charset=utf-8'
} else {
    $refBody = @{ ref = "refs/heads/$Branch"; sha = $commit.sha } | ConvertTo-Json -Compress
    $null = Invoke-RestMethod -Uri "$ApiBase/repos/$owner/$RepoName/git/refs" -Headers $Headers `
        -Method Post -Body $refBody -ContentType 'application/json; charset=utf-8'
}
Write-Ok 'Branch updated'

if ($defaultBranch -ne $Branch) {
    Write-Step "Switching default branch to $Branch"
    $patch = @{ default_branch = $Branch } | ConvertTo-Json -Compress
    $null = Invoke-RestMethod -Uri "$ApiBase/repos/$owner/$RepoName" -Headers $Headers `
        -Method Patch -Body $patch -ContentType 'application/json; charset=utf-8'
    Write-Ok 'Default branch switched'
}

# ---------------------------------------------------------------------------
# 6. Summary
# ---------------------------------------------------------------------------

Write-Host ''
Write-Host 'Publish complete' -ForegroundColor Green
Write-Host "  Repository  $($repo.html_url)"
Write-Host "  Files       $($files.Count)"
Write-Host "  Commit      $($commit.sha.Substring(0, 10)) on $Branch"
Write-Host "  CI runs     https://github.com/$owner/$RepoName/actions"
Write-Host ''
Write-Note 'CI will run automatically: gofmt / vet / race tests / coverage / smoke test / docker build.'
Write-Note "To take over with git later:"
Write-Note "  git init; git remote add origin https://github.com/$owner/$RepoName.git; git fetch origin; git checkout $Branch"
