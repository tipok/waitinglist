# Plan 12: Multi-Stage Docker Build with Distroless Image

## Overview

Create a multi-stage Dockerfile that compiles the Go binary in a builder stage and produces a minimal distroless container image. Provide Makefile targets for building `linux/amd64` and `linux/arm64` images, using `container` CLI when available and falling back to `docker`.

## Requirements

1. **Multi-stage Dockerfile** — first stage builds the Go binary, second stage copies it into `gcr.io/distroless/base-debian13:nonroot`.
2. **Architecture targets** — separate Make targets for `linux/amd64` and `linux/arm64` images, plus a convenience target that builds both.
3. **Container runtime detection** — reuse the existing `CONTAINER_RUNTIME` variable (`container` preferred, `docker` fallback).
4. **Minimal image** — the final image must contain only the static binary, the `migrations/` directory, and a default config; no build tools or source code.
5. **Non-root execution** — the distroless `nonroot` tag already runs as UID 65534; the Dockerfile must not override this.

## Design

### Dockerfile (multi-stage)

```
# Stage 1 — Builder
FROM golang:1.25 AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -a -gcflags=all="-l -B" -ldflags="-w -s" \
    -o /out/waitinglist cmd/server/main.go

# Stage 2 — Runtime
FROM gcr.io/distroless/base-debian13:nonroot
COPY --from=builder /out/waitinglist /waitinglist
COPY migrations/ /migrations/
COPY config.json /config.json
ENTRYPOINT ["/waitinglist"]
CMD ["--config", "/config.json"]
```

Key decisions:
- `CGO_ENABLED=0` produces a static binary compatible with distroless (no glibc dependency beyond what distroless provides).
- Build flags (`-ldflags="-w -s"`, `-gcflags=all="-l -B"`) match the existing `release` target for small binary size.
- `migrations/` is bundled so the container can run DB migrations on startup.
- `config.json` is copied as a default; users can mount a custom config at runtime.

### Makefile targets

| Target | Description |
|---|---|
| `docker-build\:amd64` | Build image for `linux/amd64` |
| `docker-build\:arm64` | Build image for `linux/arm64` |
| `docker-build` | Build images for both architectures |

Image naming convention: `$(BINARY_NAME):latest-<arch>` (e.g., `waitinglist:latest-amd64`).

The existing `CONTAINER_RUNTIME` variable is already defined in the Makefile and will be reused for all image build commands.

```makefile
IMAGE_NAME ?= $(BINARY_NAME)

.PHONY: docker-build\:amd64
docker-build\:amd64:
	$(CONTAINER_RUNTIME) build --build-arg TARGETARCH=amd64 -t $(IMAGE_NAME):latest-amd64 .

.PHONY: docker-build\:arm64
docker-build\:arm64:
	$(CONTAINER_RUNTIME) build --build-arg TARGETARCH=arm64 -t $(IMAGE_NAME):latest-arm64 .

.PHONY: docker-build
docker-build: docker-build\:amd64 docker-build\:arm64
```

### .dockerignore

Create a `.dockerignore` to keep the build context small:

```
bin/
.git/
.idea/
*.md
docs/
LICENSE
```

Note: `migrations/` and `config.json` must NOT be ignored since they are copied into the final image.

## Implementation Steps

1. **Create `.dockerignore`** — exclude build artifacts, VCS data, IDE files, and documentation from the build context.
2. **Rewrite `Dockerfile`** — replace the placeholder with the multi-stage build described above.
3. **Add Makefile targets** — add `IMAGE_NAME` variable and the three new phony targets (`docker-build\:amd64`, `docker-build\:arm64`, `docker-build`).
4. **Update `CLAUDE.md`** — add new Make targets to the Makefile targets table.
5. **Verify** — run `make format`, `make lint`, `make test`, then build at least one image with `make docker-build\:amd64` (or the native arch) and confirm it starts.

## Testing

Since this plan involves only build infrastructure (Dockerfile + Makefile targets), there are no unit tests to write. Verification is done by:

1. **Image builds successfully** — `make docker-build\:amd64` and/or `make docker-build\:arm64` complete without errors.
2. **Image contents are minimal** — inspect with `<runtime> run --rm <image> ls /` (distroless won't have `ls`, so verify image size is small, ~30 MB or less beyond base).
3. **Container starts** — `<runtime> run --rm <image> --help` or similar smoke test shows the binary executes inside the container.
4. **Existing tests still pass** — `make format`, `make lint`, `make test` are unaffected.

## Acceptance Criteria

- [ ] Multi-stage `Dockerfile` builds a static Go binary and copies it into `gcr.io/distroless/base-debian13:nonroot`.
- [ ] `.dockerignore` excludes unnecessary files from the build context.
- [ ] `make docker-build-amd64` builds a `linux/amd64` image.
- [ ] `make docker-build-arm64` builds a `linux/arm64` image.
- [ ] `make docker-build` builds both architecture images.
- [ ] All image build commands use `container` when available, falling back to `docker`.
- [ ] Final image runs as non-root user.
- [ ] `make format`, `make lint`, and `make test` all pass.
