#!/usr/bin/env bash
#
# Build and install helium-sync from local source
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "${SCRIPT_DIR}")"

cd "${PROJECT_DIR}"

echo "Building helium-sync..."
CGO_ENABLED=0 go build -o helium-sync ./cmd/helium-sync/

echo "Installing to /usr/bin/helium-sync..."
sudo install -Dm755 helium-sync /usr/bin/helium-sync

rm -f helium-sync

echo "Done. Installed $(helium-sync --version 2>/dev/null || echo 'helium-sync')."
