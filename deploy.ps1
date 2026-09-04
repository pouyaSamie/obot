param(
    [switch]$NoBuild,
    [switch]$NoCache,
    [int]$HealthTimeoutSeconds = 60
)

$ErrorActionPreference = 'Stop'
$containerName = 'obot-local'
$imageName = 'obot-local:dev'
$rollbackImage = 'obot-local:rollback'
$healthURL = 'http://localhost:8080/'

function Wait-ForHealth {
    $deadline = (Get-Date).AddSeconds($HealthTimeoutSeconds)
    do {
        try {
            $response = Invoke-WebRequest -Uri $healthURL -UseBasicParsing -TimeoutSec 5
            if ($response.StatusCode -ge 200 -and $response.StatusCode -lt 400) {
                return
            }
        } catch {
            # The process is expected to take a moment to bind its port.
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "Obot did not become healthy at $healthURL within $HealthTimeoutSeconds seconds."
}

git status --short
$revision = git rev-parse --short HEAD
Write-Host "Deploying revision $revision"

if (-not $NoBuild) {
    $buildArgs = @('--progress=plain', '-f', 'Dockerfile.local', '-t', $imageName, '-t', "obot-local:$revision")
    if ($NoCache) { $buildArgs += '--no-cache' }
    $buildArgs += '.'
    $env:DOCKER_BUILDKIT = '1'
    & docker build @buildArgs
    if ($LASTEXITCODE -ne 0) { throw 'Image build failed; the running container was not changed.' }
}

$old = docker inspect $containerName 2>$null | ConvertFrom-Json
if (-not $old -or $old.Count -ne 1) { throw "Expected one existing $containerName container." }
$newImageID = docker image inspect $imageName --format '{{.Id}}'
if ($old[0].Image -eq $newImageID) {
    Write-Host "$containerName already runs image $newImageID; no replacement needed."
    Wait-ForHealth
    exit 0
}

# Preserve runtime secrets without writing them to output, and preserve the
# named volume containing Obot data. The former image remains available for
# rollback until the next successful deployment.
$envArgs = @()
foreach ($entry in $old[0].Config.Env) {
    if (-not $entry.StartsWith('PATH=')) { $envArgs += @('--env', $entry) }
}
docker tag $old[0].Image $rollbackImage

function Start-Obot([string]$image) {
    & docker run -d `
        --name $containerName `
        -p 8080:8080 `
        @envArgs `
        --mount 'type=volume,src=obot-local-data,dst=/data' `
        --mount 'type=bind,src=/run/host-services/docker.proxy.sock,dst=/var/run/docker.sock' `
        $image
    if ($LASTEXITCODE -ne 0) { throw "Failed to start $containerName from $image." }
}

docker stop $containerName | Out-Null
docker rm $containerName | Out-Null
try {
    Start-Obot $imageName
    Wait-ForHealth
} catch {
    docker rm -f $containerName 2>$null | Out-Null
    Start-Obot $rollbackImage
    Wait-ForHealth
    throw "Deployment failed and $containerName was rolled back: $($_.Exception.Message)"
}

docker ps --filter "name=$containerName" --format '{{.Names}} {{.Image}} {{.Status}}'
Write-Host "Deployment of $revision succeeded."
