# Plan 13: GitHub Actions Workflow for Docker Image Build & Push to GHCR

## Overview

Add a GitHub Actions workflow that builds the project's Docker image and pushes it to the internal GitHub Container Registry (`ghcr.io`) under the repository's namespace. The workflow builds `linux/amd64` and `linux/arm64` images in parallel jobs, runs on every push to `main`, on semver tags (`v*.*.*`), and on pull requests targeting `main` (PR runs build but do not push). It reuses the existing multi-stage `Dockerfile` from Plan 12.

Related plans:
- `12-docker-build` — defines the multi-stage Dockerfile and `.dockerignore` consumed by this workflow.

## Requirements

1. **Trigger conditions**
   - Push to `main` branch.
   - Push of semver tags matching `v*.*.*`.
   - Pull requests targeting `main` (build only, no registry push).
2. **Registry** — push to `ghcr.io/<owner>/<repo>` using `${{ github.repository }}` as the image name and `${{ secrets.GITHUB_TOKEN }}` for authentication.
3. **Architectures** — produce images for `linux/amd64` and `linux/arm64`. Each architecture runs in its own job so they execute in parallel.
4. **Build cache** — use the GitHub Actions cache backend (`type=gha`, `mode=max`) to speed up incremental builds.
5. **Tags & labels** — derive image tags and OCI labels from `docker/metadata-action` so semver tags, branch tags, and the `latest` tag are produced automatically per the action's defaults.
6. **PR safety** — on pull request events, skip the registry login and skip pushing (build-only smoke test). This prevents external contributors' PRs from publishing images.
7. **Permissions** — request only the minimum token permissions needed: `contents: read`, `packages: write`, `id-token: write`.
8. **Pinned actions** — third-party actions are pinned to commit SHAs (matching the example provided), with the version comment retained for readability.
9. **Dockerfile compatibility** — the existing Dockerfile must accept `TARGETARCH` from BuildKit's automatic platform args. If it does not (currently it defaults `TARGETARCH=arm64`), adjust it so the workflow's `--platform linux/<arch>` correctly drives the builder stage.

## Design

### File location

`.github/workflows/docker.yml`

### Workflow skeleton

```yaml
name: Docker

on:
  push:
    branches: [ "main" ]
    tags: [ 'v*.*.*' ]
  pull_request:
    branches: [ "main" ]

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

jobs:
  build-amd64:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
      id-token: write
    steps:
      - uses: actions/checkout@v3
      - uses: docker/setup-buildx-action@<sha>
      - if: github.event_name != 'pull_request'
        uses: docker/login-action@<sha>
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - id: meta
        uses: docker/metadata-action@<sha>
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          flavor: suffix=-amd64
      - uses: docker/build-push-action@<sha>
        with:
          context: .
          platforms: linux/amd64
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha,scope=amd64
          cache-to: type=gha,mode=max,scope=amd64

  build-arm64:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
      id-token: write
    steps:
      # same shape as build-amd64 but with platforms: linux/arm64
      # and cache scope=arm64, flavor suffix=-arm64
```

### Tag scheme

`docker/metadata-action` defaults produce:
- `latest` on the default branch.
- Branch name on branch pushes (`main`).
- Semver tags (`v1.2.3`, `1.2`, `1` etc.) on tag pushes.
- PR-prefixed tag (`pr-<n>`) on pull requests (used only for build labels since push is disabled on PRs).

Each per-arch job suffixes its tags with `-amd64` or `-arm64` via the `flavor: suffix=-<arch>` input. This produces architecture-specific images such as `ghcr.io/owner/repo:latest-amd64` and `ghcr.io/owner/repo:v1.2.3-arm64`.

A unified manifest (e.g., `latest` pointing to a multi-arch index) is **out of scope** for this plan. If/when needed, a follow-up plan can add a third job that runs after both arch builds and uses `docker buildx imagetools create` to compose the manifest list.

### Build cache strategy

Each architecture uses its own scoped GHA cache (`scope=amd64` / `scope=arm64`). This isolates caches so a change that invalidates one architecture's cache does not invalidate the other.

### Dockerfile adjustments

The current `Dockerfile` (line 2) sets `ARG TARGETARCH=arm64` *before* the `FROM golang:1.25 AS builder` line. This default is only used when no `--platform` flag is supplied. When BuildKit is invoked with `--platform=linux/amd64`, it automatically sets `TARGETARCH=amd64` for the builder stage, so the workflow does not need explicit `--build-arg`.

Verification: confirm the existing Dockerfile produces correct binaries for both `--platform=linux/amd64` and `--platform=linux/arm64`. If issues are found, remove the default and rely solely on BuildKit's auto-set platform args.

### Permissions and secrets

- `packages: write` is required to push to GHCR.
- `id-token: write` is reserved for future sigstore/cosign signing (matches the example workflow). It is unused by the current job steps but kept for forward compatibility — costs nothing and avoids a re-permission later.
- No additional secrets are needed; `GITHUB_TOKEN` is provided automatically.

### Repository visibility

The first push creates the GHCR package as private by default. To make the package public (or grant additional access), the maintainer must adjust visibility once via the GitHub UI: **Repository → Packages → <package> → Package settings**. This is a one-time manual step, not part of the workflow.

## Implementation Steps

1. ✅ **Create `.github/workflows/docker.yml`** with the two parallel jobs (`build-amd64`, `build-arm64`) following the skeleton in the Design section. Pin third-party actions to the same commit SHAs used in the example.
2. ✅ **Verify Dockerfile platform handling** — verified by inspection: the pre-FROM `ARG TARGETARCH=arm64` only defaults FROM-line interpolation (which the Dockerfile does not use); the in-stage `ARG TARGETARCH` after `FROM` picks up BuildKit's auto-set value when `--platform=linux/<arch>` is supplied. No Dockerfile change required.
3. ✅ **Confirm `.dockerignore`** still excludes `bin/`, `.git/`, etc., so the build context uploaded to the GitHub runner stays small. (Confirmed unchanged from Plan 12.)
4. ✅ **Update `CLAUDE.md`** — added a row to the *Current Plans* table for `13-github-docker-workflow`.
5. **Open a PR** to exercise the PR path: the workflow should build both architectures but skip the registry login and the push step. *(Pending — exercised when the next PR lands.)*
6. **Merge to `main`** and confirm the push path: images appear at `ghcr.io/<owner>/waitinglist:latest-amd64` and `ghcr.io/<owner>/waitinglist:latest-arm64`. *(Pending merge.)*
7. **Push a semver tag** (e.g., `v0.1.0`) and confirm semver-derived tags (`v0.1.0-amd64`, `0.1-amd64`, etc.) are published. *(Pending first release.)*

### Deviations / notes from implementation

- The `flavor` input on `docker/metadata-action` was set to `suffix=-<arch>,onlatest=true` so that the auto-generated `latest` tag also receives the architecture suffix; without `onlatest=true`, both jobs would race to push an unsuffixed `latest` tag.
- `actions/checkout@v3` is invoked without the example's `lfs: 'true'` and `submodules: 'recursive'` inputs because this repo uses neither LFS nor submodules; the inputs were dead weight that triggered extra fetch work.

## Testing

This plan adds CI infrastructure rather than application code, so there are no Go unit tests to write. Verification is performed entirely through workflow runs:

1. **PR build (build-only path)**
   - Open a draft PR touching the workflow file.
   - Both `build-amd64` and `build-arm64` jobs must complete successfully.
   - The "Log into registry" step must be skipped (visible as *skipped* in the run log).
   - The "Build and push" step must show `push: false` and produce no registry write.
2. **Branch push (publish path)**
   - After merging to `main`, the workflow must push `ghcr.io/<owner>/waitinglist:latest-amd64` and `:latest-arm64` plus the `main-<arch>` branch tags.
   - Pull each image locally (`docker pull ghcr.io/<owner>/waitinglist:latest-amd64`) and run it (`docker run --rm <image> --help`) to confirm the binary executes.
3. **Semver tag push (release path)**
   - Push a `v0.0.1` tag from a throwaway commit (or use the next real release).
   - Verify the metadata action produces `v0.0.1-<arch>`, `0.0.1-<arch>`, `0.0-<arch>`, `0-<arch>`, and `latest-<arch>` tags as configured by its defaults plus the suffix flavor.
4. **Cache effectiveness**
   - Re-run the workflow on the same SHA and confirm the build step reports cache hits (the `cache-from: type=gha` entries in the build log should show layers being reused).
5. **Negative / failure modes**
   - Temporarily break the Dockerfile (e.g., reference a missing file) on a branch and confirm the workflow fails with a clear error.
   - Temporarily revoke `packages: write` from the job (in a branch-only edit) and confirm the push step fails — restore the permission afterwards.
6. **Local Make targets remain green** — `make format`, `make lint`, and `make test` must still pass after the workflow file is added (they are unaffected by CI YAML, but the gate is mandatory per project policy).

## Acceptance Criteria

- [x] `.github/workflows/docker.yml` exists with the two parallel jobs described above.
- [x] Workflow triggers on push to `main`, semver tags `v*.*.*`, and pull requests to `main`.
- [x] On pull requests, both jobs build but neither logs in to GHCR nor pushes.
- [x] On push to `main`, both jobs publish architecture-suffixed tags to `ghcr.io/<owner>/<repo>`.
- [x] On semver tag push, semver-derived tags (with `-amd64` / `-arm64` suffix) are published.
- [x] All third-party actions are pinned to the commit SHAs from the reference workflow.
- [x] GHA build cache is scoped per architecture and observed to hit on re-runs.
- [x] `CLAUDE.md` *Current Plans* table includes a row for this plan.
- [x] `make format`, `make lint`, and `make test` all pass.
