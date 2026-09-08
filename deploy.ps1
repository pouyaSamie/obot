param(
    [switch]$NoBuild,
    [switch]$NoCache,
    [int]$HealthTimeoutSeconds = 60
)

$ErrorActionPreference = 'Stop'

$containerName = 'obot-local'
$imageName = 'obot-local:dev'
$rollbackImage = 'obot-local:rollback'
$dataVolume = 'obot-local-data'
$backupDir = Join-Path (Get-Location) '.obot-backups'
$script:backupFile = $null
$healthURL = 'http://localhost:8080/'

# Obot normally blocks MCP servers resolving to RFC1918/private IPs.
# We intentionally allow trusted internal MCP servers such as:
# https://phone.tajan.local/mcp
$privateMcpSettingName = 'OBOT_SERVER_DISALLOW_PRIVATE_IPMCP'
$privateMcpSettingValue = 'false'

function Wait-ForHealth {
    $deadline = (Get-Date).AddSeconds($HealthTimeoutSeconds)

    do {
        try {
            $response = Invoke-WebRequest `
                -Uri $healthURL `
                -UseBasicParsing `
                -SkipHttpErrorCheck `
                -TimeoutSec 5

            # Obot does not expose a route at /.
            # A response below HTTP 500 proves the HTTP server is running.
            if ($response.StatusCode -lt 500) {
                return
            }
        }
        catch {
            # The process may take a moment to bind to the port.
        }

        Start-Sleep -Seconds 2

    } while ((Get-Date) -lt $deadline)

    throw "Obot did not become healthy at $healthURL within $HealthTimeoutSeconds seconds."
}


function Assert-ImagePersistence([string]$image) {

    $imageHome = docker image inspect $image `
        --format '{{range .Config.Env}}{{println .}}{{end}}' |
        Select-String '^HOME=' |
        Select-Object -Last 1

    $imagePgData = docker image inspect $image `
        --format '{{range .Config.Env}}{{println .}}{{end}}' |
        Select-String '^PGDATA=' |
        Select-Object -Last 1

    $imageVolumes = docker image inspect $image `
        --format '{{json .Config.Volumes}}'

    if ($LASTEXITCODE -ne 0) {
        throw "Could not inspect persistence configuration for '$image'."
    }

    $homeValue = if ($imageHome) {
        ($imageHome.ToString() -split '=', 2)[1]
    }
    else {
        ''
    }

    $pgDataValue = if ($imagePgData) {
        ($imagePgData.ToString() -split '=', 2)[1]
    }
    else {
        ''
    }

    Write-Host "Persistence contract:"
    Write-Host "  HOME    : $homeValue"
    Write-Host "  PGDATA  : $pgDataValue"
    Write-Host "  Volumes : $imageVolumes"

    if ($homeValue -ne '/data') {
        throw "Unsafe image: HOME must be /data, got '$homeValue'."
    }

    if ($pgDataValue -ne '/data/postgresql') {
        throw "Unsafe image: PGDATA must be /data/postgresql, got '$pgDataValue'."
    }

    if ($imageVolumes -notmatch '"/data"') {
        throw 'Unsafe image: /data is not declared as a Docker volume.'
    }
}


function Assert-RunningPersistence {

    $mountedVolume = docker inspect $containerName `
        --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}'

    if ($LASTEXITCODE -ne 0) {
        throw "Could not inspect $containerName persistence mount."
    }

    if ($mountedVolume.Trim() -ne $dataVolume) {
        throw "Running Obot is not using expected volume '$dataVolume' at /data."
    }

    Write-Host "Persistence mount verified: $dataVolume -> /data"
}


function Backup-DataVolume([string]$image) {

    New-Item `
        -ItemType Directory `
        -Force `
        -Path $backupDir |
        Out-Null

    $timestamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $backupName = "obot-data-$timestamp.tar.gz"
    $script:backupFile = Join-Path $backupDir $backupName

    Write-Host "Creating offline Obot data backup:"
    Write-Host "  $script:backupFile"

    & docker run --rm `
        --entrypoint sh `
        --mount "type=volume,src=$dataVolume,dst=/data,readonly" `
        --mount "type=bind,src=$backupDir,dst=/backup" `
        $image `
        -c "cd /data && tar czf /backup/$backupName ."

    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $script:backupFile)) {
        throw 'Failed to back up the Obot data volume.'
    }

    Get-ChildItem `
        -LiteralPath $backupDir `
        -Filter 'obot-data-*.tar.gz' |
        Sort-Object LastWriteTime -Descending |
        Select-Object -Skip 5 |
        Remove-Item -Force

    Write-Host 'Backup completed.'
}


function Restore-DataVolume([string]$image) {

    if (-not $script:backupFile -or -not (Test-Path -LiteralPath $script:backupFile)) {
        throw 'No pre-deployment data backup is available for rollback.'
    }

    $backupName = Split-Path $script:backupFile -Leaf

    Write-Host "Restoring Obot data volume from:"
    Write-Host "  $script:backupFile"

    & docker run --rm `
        --entrypoint sh `
        --mount "type=volume,src=$dataVolume,dst=/data" `
        --mount "type=bind,src=$backupDir,dst=/backup,readonly" `
        $image `
        -c "find /data -mindepth 1 -maxdepth 1 -exec rm -rf {} + && tar xzf /backup/$backupName -C /data"

    if ($LASTEXITCODE -ne 0) {
        throw 'Failed to restore the Obot data volume.'
    }

    Write-Host 'Data volume restore completed.'
}


# ----------------------------------------------------------------------
# Show repository state / revision
# ----------------------------------------------------------------------

git status --short

$revision = git rev-parse --short HEAD

if ($LASTEXITCODE -ne 0) {
    throw 'Could not determine the current Git revision.'
}

Write-Host "Deploying revision $revision"


# ----------------------------------------------------------------------
# Build image
# ----------------------------------------------------------------------

if (-not $NoBuild) {

    $buildArgs = @(
        '--progress=plain',
        '-f', 'Dockerfile.local',
        '-t', $imageName,
        '-t', "obot-local:$revision"
    )

    if ($NoCache) {
        $buildArgs += '--no-cache'
    }

    $buildArgs += '.'

    $env:DOCKER_BUILDKIT = '1'

    & docker build @buildArgs

    if ($LASTEXITCODE -ne 0) {
        throw 'Image build failed; the running container was not changed.'
    }
}


# ----------------------------------------------------------------------
# Inspect existing container
# ----------------------------------------------------------------------

$old = docker inspect $containerName 2>$null | ConvertFrom-Json

if (-not $old -or $old.Count -ne 1) {
    throw "Expected one existing $containerName container."
}


# ----------------------------------------------------------------------
# Check target image
# ----------------------------------------------------------------------

$newImageID = docker image inspect $imageName --format '{{.Id}}'

if ($LASTEXITCODE -ne 0 -or -not $newImageID) {
    throw "Docker image '$imageName' does not exist."
}

Assert-ImagePersistence $imageName

docker volume inspect $dataVolume *> $null

if ($LASTEXITCODE -ne 0) {
    Write-Host "Creating persistent Docker volume $dataVolume..."
    docker volume create $dataVolume | Out-Null

    if ($LASTEXITCODE -ne 0) {
        throw "Failed to create persistent Docker volume '$dataVolume'."
    }
}


# ----------------------------------------------------------------------
# Check whether required runtime configuration changed
# ----------------------------------------------------------------------

$currentPrivateMcpSetting = $old[0].Config.Env |
    Where-Object {
        $_ -like "$privateMcpSettingName=*"
    } |
    Select-Object -Last 1

$requiredPrivateMcpSetting =
    "$privateMcpSettingName=$privateMcpSettingValue"

$runtimeConfigChanged =
    $currentPrivateMcpSetting -ne $requiredPrivateMcpSetting

if ($runtimeConfigChanged) {
    Write-Host "Runtime configuration needs update:"
    Write-Host "  $requiredPrivateMcpSetting"
}


# ----------------------------------------------------------------------
# Skip replacement only when BOTH image and runtime config are unchanged
# ----------------------------------------------------------------------

if (
    ($old[0].Image -eq $newImageID) -and
    (-not $runtimeConfigChanged)
) {
    Write-Host "$containerName already runs image $newImageID with the required runtime configuration."
    Write-Host "No replacement needed."

    Wait-ForHealth

    exit 0
}


# ----------------------------------------------------------------------
# Preserve existing environment variables
#
# We deliberately remove:
#   PATH
#   OBOT_SERVER_DISALLOW_PRIVATE_IPMCP
#
# PATH comes from the image.
# The MCP setting is re-added explicitly below.
# ----------------------------------------------------------------------

$envArgs = @()

foreach ($entry in $old[0].Config.Env) {

    if (
        -not $entry.StartsWith('PATH=') -and
        -not $entry.StartsWith('HOME=') -and
        -not $entry.StartsWith('PGDATA=') -and
        -not $entry.StartsWith('XDG_CACHE_HOME=') -and
        -not $entry.StartsWith('POSTGRES_USER=') -and
        -not $entry.StartsWith('POSTGRES_PASSWORD=') -and
        -not $entry.StartsWith('POSTGRES_DB=') -and
        -not $entry.StartsWith("$privateMcpSettingName=")
    ) {
        $envArgs += @(
            '--env',
            $entry
        )
    }
}


# ----------------------------------------------------------------------
# Explicit Obot runtime configuration
# ----------------------------------------------------------------------

$envArgs += @(
    '--env',
    "$privateMcpSettingName=$privateMcpSettingValue"
)


# ----------------------------------------------------------------------
# Preserve current image for rollback
# ----------------------------------------------------------------------

Write-Host "Saving current image as rollback image..."

docker tag $old[0].Image $rollbackImage

if ($LASTEXITCODE -ne 0) {
    throw 'Failed to create rollback image.'
}


# ----------------------------------------------------------------------
# Function to start Obot
# ----------------------------------------------------------------------

function Start-Obot([string]$image) {

    Write-Host "Starting $containerName using $image..."

    & docker run -d `
        --name $containerName `
        -p 8080:8080 `
        @envArgs `
        --mount "type=volume,src=$dataVolume,dst=/data" `
        --mount 'type=bind,src=/run/host-services/docker.proxy.sock,dst=/var/run/docker.sock' `
        $image

    if ($LASTEXITCODE -ne 0) {
        throw "Failed to start $containerName from $image."
    }
}


# ----------------------------------------------------------------------
# Stop old container
# ----------------------------------------------------------------------

Write-Host "Stopping existing container..."

docker stop $containerName | Out-Null

if ($LASTEXITCODE -ne 0) {
    throw "Failed to stop $containerName."
}

Backup-DataVolume $imageName


Write-Host "Removing existing container..."

docker rm $containerName | Out-Null

if ($LASTEXITCODE -ne 0) {
    throw "Failed to remove $containerName."
}


# ----------------------------------------------------------------------
# Deploy new image
# ----------------------------------------------------------------------

try {

    Start-Obot $imageName

    Write-Host "Waiting for Obot health check..."

    Wait-ForHealth

}
catch {

    $deploymentError = $_.Exception.Message

    Write-Warning "New deployment failed."
    Write-Warning "Rolling back to $rollbackImage..."

    docker rm -f $containerName 2>$null | Out-Null

    Start-Obot $rollbackImage

    Wait-ForHealth

    throw "Deployment failed and $containerName was rolled back: $deploymentError"
}


# ----------------------------------------------------------------------
# Show status
# ----------------------------------------------------------------------

docker ps `
    --filter "name=$containerName" `
    --format '{{.Names}} {{.Image}} {{.Status}}'


# ----------------------------------------------------------------------
# Verify private MCP configuration
# ----------------------------------------------------------------------

Write-Host ""
Write-Host "Verifying Obot MCP private-IP configuration..."

$currentRuntimeSetting = docker inspect $containerName `
    --format '{{range .Config.Env}}{{println .}}{{end}}' |
    Select-String "^$privateMcpSettingName="

if (-not $currentRuntimeSetting) {
    throw "$privateMcpSettingName was not found in the running container."
}

Write-Host $currentRuntimeSetting


# ----------------------------------------------------------------------
# Complete
# ----------------------------------------------------------------------

Write-Host ""
Write-Host "Deployment of revision $revision succeeded."
Write-Host "Internal/private MCP servers are enabled."