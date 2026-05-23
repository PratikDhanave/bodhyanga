# syntax=docker/dockerfile:1.7
#
# Multi-stage build for cmd/care. Final image is distroless (~2 MB layer for
# the binary on top of distroless/static-debian12:nonroot) and runs as a
# non-root user with a read-only root filesystem.

FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/care ./cmd/care

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/care /care
EXPOSE 8088
USER nonroot:nonroot
ENTRYPOINT ["/care"]
CMD ["-addr=:8088"]
