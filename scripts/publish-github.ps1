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
    [string]$Tag = '',
    [string]$TagMessage = '',
    # Optional: create the tag at this existing commit instead of the new one.
    # Used to mark a historical commit (e.g. the last version that still
    # contained the original biased algorithm).
    [string]$TagAt = '',
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$ApiBase = 'https://api.github.com'
$RepoRoot = Split-Path -Parent $PSScriptRoot

# Walk the working tree instead of keeping a hand-written file list: a stale
# list silently drops files (that is exactly how .github/workflows/ci.yml was
# missed on the first push), and directory walking picks up new files for free.
$ExcludeDirs = @('.git', '.build', 'build', 'dist', 'node_modules', 'secrets', '.idea', '.vscode')

# Second line of defense: never upload these even if they slip through.
$ForbiddenPatterns = @('*.exe', '*.log', '*.tmp', '.env', '*.pem', '*.key', '*.p12', '*.pfx', 'secrets/*')

function Write-Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Write-Ok($msg) { Write-Host "    $msg" -ForegroundColor Green }
function Write-Note($msg) { Write-Host "    $msg" -ForegroundColor Yellow }

# GitHub occasionally answers 5xx on the Git Data endpoints. Those are transient,
# so retry with exponential backoff instead of failing a 30-file upload halfway.
function Invoke-GitHubApi {
    param(
        [Parameter(Mandatory = $true)][string]$Uri,
        [Parameter(Mandatory = $true)][string]$Method,
        $Body,
        [string]$ContentType = 'application/json; charset=utf-8',
        [int]$MaxAttempts = 5
    )
    for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
        try {
            $params = @{
                Uri = $Uri
                Headers = $Headers
                Method = $Method
                TimeoutSec = 60
            }
            if ($null -ne $Body) {
                $params['Body'] = $Body
                $params['ContentType'] = $ContentType
            }
            return Invoke-RestMethod @params
        } catch {
            $status = $_.Exception.Response.StatusCode.value__
            $retryable = ($status -ge 500) -or ($status -eq 429) -or ($status -eq 403) -or ($null -eq $status)
            if (-not $retryable -or $attempt -eq $MaxAttempts) { throw }
            # A 403 here is usually GitHub's secondary rate limit (too many writes
            # in a short window). It needs a longer backoff than a plain 5xx,
            # otherwise the retry is rejected again immediately.
            $wait = [Math]::Pow(2, $attempt)
            if ($status -eq 403) { $wait = 15 * $attempt }
            Write-Note "HTTP $status on attempt $attempt/$MaxAttempts, retrying in ${wait}s ..."
            Start-Sleep -Seconds $wait
        }
    }
}

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
$skipped = @()

$allFiles = Get-ChildItem -LiteralPath $RepoRoot -Recurse -File -Force
foreach ($f in $allFiles) {
    $rel = $f.FullName.Substring($RepoRoot.Length).TrimStart('\', '/') -replace '\\', '/'

    # Skip anything under an excluded directory (match at any path depth).
    $parts = $rel -split '/'
    $inExcluded = $false
    for ($i = 0; $i -lt [Math]::Max(1, $parts.Count - 1); $i++) {
        if ($ExcludeDirs -contains $parts[$i]) { $inExcluded = $true; break }
    }
    if ($inExcluded) { $skipped += $rel; continue }

    $forbidden = $false
    foreach ($pat in $ForbiddenPatterns) {
        if ($rel -like $pat) { $forbidden = $true; break }
    }
    if ($forbidden) {
        Write-Note "SKIPPED (forbidden pattern): $rel"
        $skipped += $rel
        continue
    }

    $bytes = [IO.File]::ReadAllBytes($f.FullName)
    $totalBytes += $bytes.Length
    $files += [pscustomobject]@{
        Path = $rel
        Bytes = $bytes
        Size = $bytes.Length
    }
}

Write-Ok ("{0} files, {1:N1} KB total" -f $files.Count, ($totalBytes / 1KB))
foreach ($f in $files) { Write-Host ("      {0,10:N0} B  {1}" -f $f.Size, $f.Path) }
if ($skipped.Count -gt 0) {
    Write-Note ("skipped {0} path(s): {1}" -f $skipped.Count, ($skipped -join ', '))
}

# Fail loudly if the CI workflow is absent: without it nothing runs on push.
if (-not ($files | Where-Object { $_.Path -eq '.github/workflows/ci.yml' })) {
    throw ".github/workflows/ci.yml is missing from the collected file set"
}

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

Write-Step "Resolving repository $owner/$RepoName"
# Check first with a GET instead of attempting a POST that is expected to fail:
# GitHub applies a secondary rate limit to content creation, and every doomed
# create call counts against it. A read costs far less than a failed write.
$repo = $null
try {
    $repo = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName" -Method Get
    Write-Ok "Repository exists, pushing into it: $($repo.html_url)"
} catch {
    $status = $_.Exception.Response.StatusCode.value__
    if ($status -ne 404) {
        throw "Failed to read repository (HTTP $status): $($_.ErrorDetails.Message)"
    }
}

if (-not $repo) {
    Write-Step "Creating repository $owner/$RepoName"
    $repoPayload = @{
        name = $RepoName
        description = $Description
        private = [bool]$Private
        has_issues = $true
        has_wiki = $false
        auto_init = $false
    } | ConvertTo-Json
    $repo = Invoke-GitHubApi -Uri "$ApiBase/user/repos" -Method Post -Body $repoPayload
    Write-Ok "Repository created: $($repo.html_url)"
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
    $blob = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName/git/blobs" -Method Post -Body $body
    $treeItems += @{
        path = $f.Path
        mode = '100644'
        type = 'blob'
        sha = $blob.sha
    }
    Write-Host ("      ok  {0}" -f $f.Path)
}

# ---------------------------------------------------------------------------
# 4b. Delete files that no longer exist locally
#
# The tree API is additive: giving it only the current files would leave deleted
# files behind on the remote forever. So compare against the remote tree and
# submit an explicit deletion entry (sha = null) for every stale path.
#
# Deletions only work when the new tree is based on the current one (base_tree),
# otherwise GitHub rejects the request with GitRPC::BadObjectState.
# ---------------------------------------------------------------------------

$baseTreeSha = $null
try {
    $branchRef = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName/git/ref/heads/$Branch" -Method Get
    $branchCommit = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName/git/commits/$($branchRef.object.sha)" -Method Get
    $baseTreeSha = $branchCommit.tree.sha
    Write-Ok "base tree = $($baseTreeSha.Substring(0, 10))"
} catch {
    Write-Ok "branch $Branch not found; this will be the root commit (no base tree)"
}

$deleted = @()
if ($baseTreeSha) {
    $remoteTree = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName/git/trees/$baseTreeSha`?recursive=1" -Method Get
    $localPaths = @{}
    foreach ($f in $files) { $localPaths[$f.Path] = $true }

    foreach ($item in $remoteTree.tree) {
        if ($item.type -ne 'blob') { continue }
        if ($localPaths.ContainsKey($item.path)) { continue }
        $deleted += $item.path
        $treeItems += @{
            path = $item.path
            mode = '100644'
            type = 'blob'
            sha = $null
        }
    }
    if ($deleted.Count -gt 0) {
        Write-Ok ("deleting {0} stale file(s):" -f $deleted.Count)
        foreach ($d in $deleted) { Write-Host ("      del {0}" -f $d) }
    } else {
        Write-Ok 'no stale files to delete'
    }
}

# ---------------------------------------------------------------------------
# 5. Create tree + commit, then update the branch ref
# ---------------------------------------------------------------------------

Write-Step 'Creating tree'
$treePayload = @{ tree = $treeItems }
if ($baseTreeSha) { $treePayload['base_tree'] = $baseTreeSha }
$treeBody = $treePayload | ConvertTo-Json -Depth 6 -Compress
$newTree = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName/git/trees" -Method Post -Body $treeBody
Write-Ok "tree sha = $($newTree.sha)"

# The branch HEAD (if any) becomes the parent commit. It was already resolved
# in step 4b while computing the base tree, so reuse it instead of asking twice.
$parentSha = $null
if ($baseTreeSha) {
    $parentSha = $branchRef.object.sha
    Write-Ok "parent commit = $($parentSha.Substring(0, 10))"
} else {
    Write-Ok "branch $Branch does not exist yet; this will be the root commit"
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

$commit = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName/git/commits" -Method Post -Body $commitBody
Write-Ok "commit sha = $($commit.sha)"

Write-Step "Updating branch ref $Branch"
if ($parentSha) {
    $refBody = @{ sha = $commit.sha; force = $false } | ConvertTo-Json -Compress
    $null = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName/git/refs/heads/$Branch" -Method Patch -Body $refBody
} else {
    $refBody = @{ ref = "refs/heads/$Branch"; sha = $commit.sha } | ConvertTo-Json -Compress
    $null = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName/git/refs" -Method Post -Body $refBody
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
# 5b. Optional annotated tag (useful for marking milestones, e.g. the
#     commit that still contained the original biased algorithm)
# ---------------------------------------------------------------------------

if ($Tag) {
    # Tag an existing commit when -TagAt is given, otherwise tag the new commit.
    $tagTarget = $commit.sha
    if ($TagAt) { $tagTarget = $TagAt }
    Write-Step "Creating tag $Tag -> $($tagTarget.Substring(0, 10))"

    $tagBody = @{
        tag = $Tag
        message = if ($TagMessage) { $TagMessage } else { "Release $Tag" }
        object = $tagTarget
        type = 'commit'
        tagger = @{
            name = $CommitAuthorName
            email = $CommitAuthorEmail
            date = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
        }
    } | ConvertTo-Json -Depth 5 -Compress

    $tagObj = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName/git/tags" -Method Post -Body $tagBody
    Write-Ok "tag object = $($tagObj.sha)"

    $tagRefBody = @{ ref = "refs/tags/$Tag"; sha = $tagObj.sha } | ConvertTo-Json -Compress
    $null = Invoke-GitHubApi -Uri "$ApiBase/repos/$owner/$RepoName/git/refs" -Method Post -Body $tagRefBody
    Write-Ok "tag $Tag created"
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
