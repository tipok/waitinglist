# Stage 1 — Builder
ARG TARGETARCH=arm64
FROM golang:1.25 AS builder
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -a -gcflags=all="-l -B" -ldflags="-w -s" \
    -o /out/waitinglist cmd/server/main.go

# Stage 2 — Runtime
FROM gcr.io/distroless/base-debian13:nonroot
COPY --from=builder /out/waitinglist /waitinglist
COPY migrations/ /migrations/

ENV WL_PORT=8081

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/waitinglist", "--health-check"]

ENTRYPOINT ["/waitinglist"]
CMD ["--config", "/config.json"]
