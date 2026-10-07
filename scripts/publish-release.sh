#!/usr/bin/env bash
set -euo pipefail

: "${GH_REPO:?GH_REPO is required}"
: "${RELEASE_TAG:?RELEASE_TAG is required}"

script_dir="$(cd "$(dirname "$0")" && pwd)"
python3 - "$script_dir" "${1:?artifact directory is required}" <<'PY'
import os
from pathlib import Path
import sys
sys.path.insert(0, sys.argv[1])
from package_release import validate_tag, verify_artifacts
validate_tag(os.environ["RELEASE_TAG"])
verify_artifacts(Path(sys.argv[2]))
PY

cd "$1"
assets=(kongweave_darwin_amd64.tar.gz kongweave_darwin_arm64.tar.gz kongweave_linux_amd64.tar.gz kongweave_linux_arm64.tar.gz checksums.txt)

options=(--repo "$GH_REPO" --verify-tag --title "$RELEASE_TAG" --generate-notes --draft)
version_core="${RELEASE_TAG%%+*}"
if [[ "$version_core" == *-* ]]; then
  options+=(--prerelease)
fi

# Attach all assets to a draft so a failed upload never exposes a partial release.
# Creating an existing release fails; published assets are never replaced on reruns.
gh release create "$RELEASE_TAG" "${assets[@]}" "${options[@]}"
gh release edit "$RELEASE_TAG" --repo "$GH_REPO" --draft=false
