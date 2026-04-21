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
