# syntax=docker/dockerfile:1.7

# ---- build stage ---------------------------------------------------------
FROM golang:1.25.10-alpine AS build

WORKDIR /src

# Install ca-certificates so we can copy them into the scratch image; the
# Go toolchain itself is already present in the golang:alpine base.
RUN apk add --no-cache ca-certificates

# Cache module downloads in a separate layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0 GOFLAGS="-trimpath" LDFLAGS="-s -w"
RUN go build -ldflags="${LDFLAGS}" -o /out/notifycat-server   ./cmd/notifycat-server
RUN go build -ldflags="${LDFLAGS}" -o /out/notifycat-mapping  ./cmd/notifycat-mapping
RUN go build -ldflags="${LDFLAGS}" -o /out/notifycat-migrate  ./cmd/notifycat-migrate
RUN mkdir -p /out/data

# ---- runtime stage -------------------------------------------------------
FROM scratch

# TLS roots so the Slack HTTPS client works.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Statically linked binaries (CGO_ENABLED=0); ENTRYPOINT left empty so the
# container can run any of the three.
COPY --from=build /out/notifycat-server  /usr/local/bin/notifycat-server
COPY --from=build /out/notifycat-mapping /usr/local/bin/notifycat-mapping
COPY --from=build /out/notifycat-migrate /usr/local/bin/notifycat-migrate
COPY --from=build --chown=65532:65532 /out/data /data

EXPOSE 8080
ENV DATABASE_URL=file:/data/notifycat.db

# Distroless-style non-root UID; works on scratch because scratch has no
# /etc/passwd to consult.
USER 65532:65532

ENTRYPOINT []
CMD ["/usr/local/bin/notifycat-server"]
