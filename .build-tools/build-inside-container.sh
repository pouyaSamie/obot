#!/usr/bin/env bash
set -Eeuo pipefail

echo
echo "Persistent caches:"
echo "  GOMODCACHE : /cache/go-mod"
echo "  GOCACHE    : /cache/go-build"
echo "  pnpm store : /cache/pnpm-store"
echo "  npm cache  : /cache/npm-cache"
echo

rm -rf /work/src
mkdir -p /work/src /out

tar \
  --exclude='./.git' \
  --exclude='./.build-output' \
  --exclude='./.build-tools' \
  --exclude='./ui/user/node_modules' \
  --exclude='./bin' \
  -C /src \
  -cf - . |
tar -C /work/src -xf -

cd /work/src

export GOMODCACHE=/cache/go-mod
export GOCACHE=/cache/go-build
export GOPROXY="${OBOT_GO_PROXY}"
export GOSUMDB=off
export npm_config_cache=/cache/npm-cache

echo "Go environment:"
go env GOPROXY GOMODCACHE GOCACHE
echo

echo "Preparing UI dependencies..."
cd /work/src/ui/user

pnpm config set store-dir /cache/pnpm-store
pnpm config set registry "${OBOT_PNPM_REGISTRY}"

# Prefer the persistent K: cache. The registry is only used for cache misses.
pnpm install   --frozen-lockfile   --prefer-offline   --fetch-retries 3   --fetch-retry-mintimeout 1000   --fetch-retry-maxtimeout 10000   --network-concurrency 8

pnpm run build

echo
echo "Preparing Go dependencies..."
cd /work/src
go mod download all

echo
echo "Building customized Obot binary..."
make build

echo
echo "Copying build artifacts..."
rm -rf /out/*
mkdir -p /out/ui-build

cp /work/src/bin/obot /out/obot
cp -a /work/src/ui/user/build/. /out/ui-build/

echo
echo "Build artifacts:"
ls -lh /out/obot
du -sh /out/ui-build
echo
echo "Linux source build completed."
