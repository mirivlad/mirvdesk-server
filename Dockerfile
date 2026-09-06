ARG RUSTDESK_SERVER_VERSION=1.1.16
ARG BUILDPLATFORM=linux/amd64
FROM rustdesk/rustdesk-server:${RUSTDESK_SERVER_VERSION} AS rustdesk

FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG RUSTDESK_SERVER_VERSION=1.1.16
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.rustDeskServerVersion=${RUSTDESK_SERVER_VERSION}" \
    -o /out/mirvdesk-server ./cmd/mirvdesk-server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=rustdesk /usr/bin/hbbs /usr/local/bin/hbbs
COPY --from=rustdesk /usr/bin/hbbr /usr/local/bin/hbbr
COPY --from=build /out/mirvdesk-server /usr/local/bin/mirvdesk-server
VOLUME ["/data"]
EXPOSE 21114/tcp 21115/tcp 21116/tcp 21116/udp 21117/tcp
ENTRYPOINT ["/usr/local/bin/mirvdesk-server"]