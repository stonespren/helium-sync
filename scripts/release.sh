#!/usr/bin/env bash
#
# helium-sync release script
# Usage: ./scripts/release.sh <version>
# Example: ./scripts/release.sh 0.2.0
#
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
log_step() { echo -e "${BLUE}[STEP]${NC} $*"; }

# -------------------------------------------------------
# Validate argument
# -------------------------------------------------------
if [[ $# -ne 1 ]]; then
    log_error "Usage: $(basename "$0") <version>"
    log_error "Example: $(basename "$0") 0.2.0"
    exit 1
fi

VERSION="$1"

if ! [[ "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log_error "Version must be in semver format: MAJOR.MINOR.PATCH (e.g. 0.2.0)"
    exit 1
fi

TAG="v${VERSION}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "${SCRIPT_DIR}")"

cd "${PROJECT_DIR}"

# -------------------------------------------------------
# Pre-flight checks
# -------------------------------------------------------
log_step "Pre-flight checks..."

# Must be on main branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "${CURRENT_BRANCH}" != "main" ]]; then
    log_error "Must be on 'main' branch (currently on '${CURRENT_BRANCH}')"
    exit 1
fi

# Working tree must be clean
if ! git diff --quiet || ! git diff --cached --quiet; then
    log_error "Working tree has uncommitted changes. Commit or stash them first."
    exit 1
fi

# Tag must not already exist
if git rev-parse "${TAG}" &>/dev/null; then
    log_error "Tag ${TAG} already exists."
    exit 1
fi

log_info "All checks passed."

# -------------------------------------------------------
# Bump version
# -------------------------------------------------------
log_step "Bumping version to ${VERSION}..."

# packaging/arch/PKGBUILD — single source of truth for the version in-tree.
# main.go uses "dev" as a fallback; the real version is injected via ldflags at build time.
PKGBUILD="${PROJECT_DIR}/packaging/arch/PKGBUILD"
CURRENT_PKGVER=$(grep -oP '(?<=pkgver=)[^\s]+' "${PKGBUILD}")
sed -i "s/pkgver=${CURRENT_PKGVER}/pkgver=${VERSION}/" "${PKGBUILD}"
# Reset pkgrel to 1 for a new upstream version
sed -i 's/pkgrel=[0-9]\+/pkgrel=1/' "${PKGBUILD}"
log_info "Updated PKGBUILD: ${CURRENT_PKGVER} -> ${VERSION}"

# Regenerate .SRCINFO from PKGBUILD (requires makepkg, Arch Linux only)
SRCINFO="${PROJECT_DIR}/packaging/arch/.SRCINFO"
if command -v makepkg &>/dev/null; then
    pushd "${PROJECT_DIR}/packaging/arch" > /dev/null
    makepkg --printsrcinfo > .SRCINFO
    popd > /dev/null
    log_info ".SRCINFO regenerated."
else
    log_warn "makepkg not found — .SRCINFO not regenerated. Update it manually on an Arch machine before pushing to AUR."
fi

# -------------------------------------------------------
# Verify build
# -------------------------------------------------------
log_step "Verifying build..."
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /tmp/helium-sync-check ./cmd/helium-sync/
rm /tmp/helium-sync-check
log_info "Build successful."

# -------------------------------------------------------
# Commit, tag, push
# -------------------------------------------------------
log_step "Committing version bump..."
git add packaging/arch/PKGBUILD packaging/arch/.SRCINFO
git commit -m "chore: release ${TAG}"

log_step "Creating tag ${TAG}..."
git tag "${TAG}" -m "Release ${TAG}"

log_step "Pushing to origin..."
git push origin main
git push origin "${TAG}"

echo
log_info "Released ${TAG}!"
echo
echo "The GitHub Actions workflow will now build the binaries and create the GitHub release."
echo "Monitor it at: https://github.com/stonespren/helium-sync/actions"
