FROM golang:1.25-bookworm AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY pkg ./pkg

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags='-s -w' -o /out/cihealth ./cmd

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /workspace && \
    chown 65532:65532 /workspace

WORKDIR /workspace
ENV HOME=/workspace

COPY --from=builder /out/cihealth /usr/local/bin/cihealth

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/cihealth"]
