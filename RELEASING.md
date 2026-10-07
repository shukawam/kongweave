# Releasing Kongweave

This document is for maintainers; installation instructions are in the [README](README.md#installation).
Pushing a version tag starts `.github/workflows/release.yml` and publishes a GitHub Release only after all checks and uploads succeed.

## First release setup

1. Confirm that the repository is available at `shukawam/kongweave`, the destination used by the installation instructions.
2. Enable GitHub Actions and allow the release job's `contents: write` permission; the workflow uses the repository's `GITHUB_TOKEN` and requires no personal access token.
3. Restrict creation and modification of `v*` tags to maintainers using repository rulesets, and review changes to release scripts and workflows before tagging.
4. Run CI on the intended release commit and review dependency updates.

The workflow derives the release destination from `github.repository`; it does not hard-code an owner or contact a Kong environment.
It targets GitHub.com hosted runners, not GitHub Enterprise Server or older self-hosted runners.

## Tag a release

Use a clean, reviewed commit that has passed CI.
Stable tags use `vMAJOR.MINOR.PATCH`; prerelease tags such as `v0.1.0-rc.1` are also supported and produce GitHub prereleases.

```sh
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

These commands trigger publication; run them only when ready to release.
After success, inspect the release assets, download the archive for your platform, verify its checksum, and confirm that `kongweave version` reports the tag.
The archive names do not include the version, so `/releases/latest/download/<asset>` stays usable across stable releases.
Use `/releases/download/<tag>/<asset>` when pinning an installation or selecting a prerelease.

## What the workflow does

The package job has read-only repository access and disables dependency caching and persisted checkout credentials.
It validates the tag, runs Go checks and release-tool tests, and packages the exact tagged commit from a clean checkout.
The four builds use `CGO_ENABLED=0`, embed the tag in `main.version`, and include the standalone executable, license, user documentation, and examples.
Only tracked public payload files are included; internal notes, source code, and local generated files are excluded.
Archives normalize timestamps and ownership, and `checksums.txt` covers every archive.
The packaged Linux amd64 executable is exercised against all four included examples before assets are passed to the publish job.

The publish job alone has `contents: write` permission.
It verifies the exact asset set and every SHA-256 checksum, creates a draft with all assets, then publishes the completed release.
The `GH_TOKEN` environment variable is set only for the publication step, and no native plan, diff, apply, or sync command runs.

## Updating dependencies

Every `uses:` reference is pinned to a full 40-character commit SHA with the corresponding release version in a comment.
Dependabot checks GitHub Actions dependencies weekly; review the upstream release and commit before merging each update, retaining full SHA pins.
Do not replace a SHA with a movable major/minor tag or branch.
For a manual update, use the official repository's latest stable release, resolve its exact tag to a commit, and verify that commit belongs to that repository.
The release Go toolchain is pinned separately in `release.yml`; update it deliberately when new patch releases are available.
CI retains Go 1.23.12 only to exercise the minimum supported language version; distributed binaries use the current pinned toolchain, Go 1.27.1.

## Local packaging and recovery

From a clean checkout with an existing local version tag, the following generates assets without publishing anything:

```sh
python3 scripts/package_release.py --tag v0.1.0 --output dist/release-check
```

This maintainer command requires Python 3.9 or later, Git, and the release Go toolchain; end users need none of them.
For byte-for-byte reproduction, use the same source commit, Go version, and Python version as the original build.
The output directory must not already exist.
Local packaging does not execute Linux binaries on macOS or validate every target platform at runtime.
The workflow does not sign or notarize macOS binaries.

If packaging or checksum validation fails, no release is created.
If an upload or final publication fails, a draft may remain; inspect or remove that draft before rerunning the failed workflow.
Reruns do not overwrite an existing release or replace published assets.
Once a release is published, use a new version tag for changes instead of moving an existing tag.

References: [GitHub release links](https://docs.github.com/en/repositories/releasing-projects-on-github/linking-to-releases), [GitHub CLI release creation](https://cli.github.com/manual/gh_release_create), and [Actions security guidance](https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions).
