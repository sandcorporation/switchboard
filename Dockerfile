# The build stage runs on the build platform and cross-compiles for the target (pure-Go SQLite, no CGO),
# so only the small runtime stage runs under emulation in multi-arch builds.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /switchboard ./cmd/switchboard
FROM alpine:3.22
RUN addgroup -S switchboard && adduser -S -G switchboard switchboard && mkdir -p /data && chown switchboard:switchboard /data
COPY --from=build /switchboard /usr/local/bin/switchboard
USER switchboard
ENV DB_PATH=/data/mail.db LISTEN_ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -q -O - http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["switchboard"]
