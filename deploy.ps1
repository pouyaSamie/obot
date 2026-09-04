# 1. Confirm the code you intend to deploy.
git status
git log -1 --oneline

# 2. Build a fresh local image from the current checkout.
docker build --progress=plain -f Dockerfile.local -t obot-local:dev .
if ($LASTEXITCODE -ne 0) { throw "Image build failed; the running container was not changed." }

# 3. Read the current runtime settings and keep a rollback image.
$old = docker inspect obot-local | ConvertFrom-Json
docker tag $old[0].Image obot-local:rollback

$envArgs = @()
foreach ($entry in $old[0].Config.Env) {
    if (-not $entry.StartsWith('PATH=')) {
        $envArgs += @('--env', $entry)
    }
}

# 4. Replace only the container. The named data volume is retained.
docker stop obot-local
docker rm obot-local

& docker run -d `
    --name obot-local `
    -p 8080:8080 `
    @envArgs `
    --mount 'type=volume,src=obot-local-data,dst=/data' `
    --mount 'type=bind,src=/run/host-services/docker.proxy.sock,dst=/var/run/docker.sock' `
    obot-local:dev

# 5. Verify the replacement.
docker ps --filter name=obot-local
Invoke-WebRequest http://localhost:8080/ -UseBasicParsing | Select-Object StatusCode