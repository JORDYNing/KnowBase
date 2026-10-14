#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
exec docker compose -p knowbase -f docker-compose.yml -f deploy/docker-compose.server.yml "$@"
