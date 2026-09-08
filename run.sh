#!/bin/bash
set -e

if [ "${1:-}" = "tunnel" ]; then
  exec tini -- obot "$@"
fi

check_postgres_active() {
  for i in {1..30}; do
    if pg_isready -q; then
      echo "PostgreSQL is active and ready!"
      return 0
    fi

    echo "Waiting for PostgreSQL to become active... ($i/30)"
    sleep 2
  done

  echo "PostgreSQL did not become active in time."
  exit 1
}

# Provider bundles normally publish a combined runtime environment through this
# file. Some bundle versions retain their individual .envrc.* files instead,
# so use those as a fallback rather than failing before Obot can start.
provider_env=/obot-providers/.envrc.providers

if [ -s "$provider_env" ]; then
  source "$provider_env"
else
  shopt -s nullglob
  provider_env_files=(/obot-providers/.envrc.*)
  shopt -u nullglob

  if [ "${#provider_env_files[@]}" -eq 0 ]; then
    echo "No provider environment files were found in /obot-providers."
    exit 1
  fi

  for provider_env_file in "${provider_env_files[@]}"; do
    source "$provider_env_file"
  done
fi

mkdir -p /data/cache

if [ -z "${OBOT_SERVER_DSN:-}" ]; then
  echo "OBOT_SERVER_DSN is not set. Starting embedded PostgreSQL..."

  if command -v docker-entrypoint.sh >/dev/null 2>&1; then
    POSTGRES_ENTRYPOINT="$(command -v docker-entrypoint.sh)"
  elif [ -x /usr/local/bin/docker-entrypoint.sh ]; then
    POSTGRES_ENTRYPOINT="/usr/local/bin/docker-entrypoint.sh"
  elif [ -x /usr/bin/docker-entrypoint.sh ]; then
    POSTGRES_ENTRYPOINT="/usr/bin/docker-entrypoint.sh"
  else
    echo "PostgreSQL docker-entrypoint.sh was not found."
    exit 1
  fi

  "$POSTGRES_ENTRYPOINT" postgres &

  check_postgres_active

  export OBOT_SERVER_DSN="postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:5432/${POSTGRES_DB}"
fi

if command -v tini >/dev/null 2>&1; then
  exec tini -- obot server
fi

exec obot server
