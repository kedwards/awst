#!/usr/bin/env bash
set -euo pipefail

# Release script for aws-tools
# Usage: ./scripts/release.sh [major|minor|patch]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
VERSION_FILE="$ROOT_DIR/VERSION"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

error() {
  echo -e "${RED}[ERROR]${NC} $*" >&2
  exit 1
}

info() {
  echo -e "${BLUE}[INFO]${NC} $*"
}

success() {
  echo -e "${GREEN}[SUCCESS]${NC} $*"
}

warn() {
  echo -e "${YELLOW}[WARN]${NC} $*"
}

# Check required commands
command -v gh >/dev/null 2>&1 || error "gh (GitHub CLI) is required. Install from https://cli.github.com/"
command -v git >/dev/null 2>&1 || error "git is required"

# Check we're in a git repo
if ! git rev-parse --git-dir >/dev/null 2>&1; then
  error "Not in a git repository"
fi

# Check for uncommitted changes
if [[ -n $(git status --porcelain) ]]; then
  error "Uncommitted changes detected. Commit or stash changes before releasing."
fi

# Check we're on main branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "$CURRENT_BRANCH" != "main" ]]; then
  warn "Not on main branch (currently on $CURRENT_BRANCH)"
  read -p "Continue anyway? (y/N): " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    exit 1
  fi
fi

# Read current version
if [[ ! -f "$VERSION_FILE" ]]; then
  error "VERSION file not found at $VERSION_FILE"
fi

CURRENT_VERSION=$(cat "$VERSION_FILE")
info "Current version: $CURRENT_VERSION"

# Parse version components
if [[ ! "$CURRENT_VERSION" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  error "Invalid version format in VERSION file: $CURRENT_VERSION (expected: X.Y.Z)"
fi

MAJOR="${BASH_REMATCH[1]}"
MINOR="${BASH_REMATCH[2]}"
PATCH="${BASH_REMATCH[3]}"

# Determine bump type
BUMP_TYPE="${1:-}"
if [[ -z "$BUMP_TYPE" ]]; then
  echo ""
  echo "Select version bump type:"
  echo "  1) patch (bug fixes)          $CURRENT_VERSION -> $MAJOR.$MINOR.$((PATCH + 1))"
  echo "  2) minor (new features)       $CURRENT_VERSION -> $MAJOR.$((MINOR + 1)).0"
  echo "  3) major (breaking changes)   $CURRENT_VERSION -> $((MAJOR + 1)).0.0"
  echo ""
  read -p "Choice [1-3]: " -n 1 -r
  echo
  case $REPLY in
    1) BUMP_TYPE="patch" ;;
    2) BUMP_TYPE="minor" ;;
    3) BUMP_TYPE="major" ;;
    *) error "Invalid choice" ;;
  esac
fi

# Calculate new version
case "$BUMP_TYPE" in
  patch)
    NEW_VERSION="$MAJOR.$MINOR.$((PATCH + 1))"
    ;;
  minor)
    NEW_VERSION="$MAJOR.$((MINOR + 1)).0"
    ;;
  major)
    NEW_VERSION="$((MAJOR + 1)).0.0"
    ;;
  *)
    error "Invalid bump type: $BUMP_TYPE (must be major, minor, or patch)"
    ;;
esac

TAG_NAME="v$NEW_VERSION"

info "New version will be: $NEW_VERSION (tag: $TAG_NAME)"
echo ""

# Confirm
read -p "Create release $TAG_NAME? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  info "Release cancelled"
  exit 0
fi

# Update VERSION file
info "Updating VERSION file..."
echo "$NEW_VERSION" > "$VERSION_FILE"

# Run tests inside the dev image so the release gate matches CI exactly
# and doesn't depend on bats/shellcheck being installed on the host.
info "Running tests (task docker:ci)..."
command -v task >/dev/null 2>&1 || error "task is required (install go-task: https://taskfile.dev)"
if ! task docker:ci; then
  error "Tests failed - aborting release. Fix the failures and re-run."
fi

# Commit version bump
info "Committing version bump..."
git add "$VERSION_FILE"
git commit -m "chore: bump version to $NEW_VERSION

Co-Authored-By: Warp <agent@warp.dev>"

# Create and push tag
info "Creating tag $TAG_NAME..."
git tag -a "$TAG_NAME" -m "Release $TAG_NAME"

info "Pushing changes and tag..."
git push origin "$CURRENT_BRANCH"
git push origin "$TAG_NAME"

# Generate release notes
info "Generating release notes..."
RELEASE_NOTES_FILE=$(mktemp)
trap 'rm -f "$RELEASE_NOTES_FILE"' EXIT

# Get commits since last tag
LAST_TAG=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo "")
if [[ -n "$LAST_TAG" ]]; then
  info "Changes since $LAST_TAG:"
  git log "$LAST_TAG..HEAD" --pretty=format:"- %s" > "$RELEASE_NOTES_FILE"
else
  info "First release - including all commits:"
  git log --pretty=format:"- %s" > "$RELEASE_NOTES_FILE"
fi

# Create GitHub release
info "Creating GitHub release..."
gh release create "$TAG_NAME" \
  --title "Release $TAG_NAME" \
  --notes-file "$RELEASE_NOTES_FILE" \
  --verify-tag

success "Release $TAG_NAME created successfully!"
echo ""
info "View release at: https://github.com/kedwards/aws-tools/releases/tag/$TAG_NAME"
echo ""
info "Users can install this version with:"
echo "  curl -sSL https://raw.githubusercontent.com/kedwards/aws-tools/main/install.sh | bash -s $TAG_NAME"
