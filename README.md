# MirvDesk Server

MirvDesk Server is the self-hosted backend for MirvDesk, an independent downstream project based on RustDesk.
It packages the RustDesk OSS rendezvous server (`hbbs`), relay server (`hbbr`), and the MirvDesk API in a single container.

## Goals

- one image and one Portainer stack;
- one persistent `/data` volume;
- direct RustDesk transport ports, with the HTTP API behind an existing nginx reverse proxy;
- a small open API for client login, address books, groups, and synchronization;
- stay compatible with the open MirvDesk/RustDesk client wherever practical.

## Quick start

```bash
docker compose up -d
```

The recommended Linux deployment uses `network_mode: host`.
Open TCP 21115, TCP+UDP 21116 and TCP 21117 to clients. The API listens on `127.0.0.1:21114` and is intended to be published through nginx HTTPS.

See [Portainer + nginx](docs/portainer-nginx.md).

## Current status

The first bootstrap provides the single-container process supervisor, `hbbs`/`hbbr` 1.1.16, health/version endpoints, Docker Compose, and GHCR build workflow.
Account authentication and address-book APIs are the next implementation step.

## API available now

- `GET /healthz`
- `GET /api/version`
- `GET /api/login-options`

## Upstream and license

RustDesk Server OSS is developed at https://github.com/rustdesk/rustdesk-server and is distributed under AGPL-3.0.
MirvDesk Server is independent from and is not endorsed by RustDesk/Purslane.
