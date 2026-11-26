# syntax=docker/dockerfile:1

# ---- Stage 1: build ----------------------------------------------------
# A full Go toolchain (~300 MB). Used only to compile; thrown away afterwards.
FROM golang:1.27-alpine AS build
WORKDIR /src

# Copy only the dependency list first. Docker caches this layer, so
# dependencies are re-downloaded only when go.mod or go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Now copy the source and build one static binary (no C libraries needed).
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- Stage 2: run ------------------------------------------------------
# Distroless: no shell, no package manager, just enough to run a static
# binary (~2 MB). Smaller image, smaller attack surface.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/server /server

# Document the port the server listens on (PORT defaults to 8080).
EXPOSE 8080

# The :nonroot image already runs as an unprivileged user; this makes it explicit.
USER nonroot:nonroot
ENTRYPOINT ["/server"]