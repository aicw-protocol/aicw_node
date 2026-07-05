# syntax=docker/dockerfile:1.4
# Multi-stage build: compile aicw-node against the local mpcium fork (AICW patches).
# Build with BuildKit and the mpcium sibling context:
#   docker build --build-context mpcium=../mpcium -t ghcr.io/aicw-protocol/aicw-node:local .

FROM golang:1.25.8-alpine AS builder
WORKDIR /build/aicw_node

# Forked mpcium (includes AICW-FORK hooks). Path must match go.mod:
#   replace github.com/fystack/mpcium => ../mpcium
COPY --from=mpcium . /build/mpcium/

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/aicw-node ./cmd/aicw-node

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata su-exec
WORKDIR /app
COPY --from=builder /out/aicw-node /usr/local/bin/aicw-node
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh && adduser -D -u 1000 node
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["start", "--help"]
