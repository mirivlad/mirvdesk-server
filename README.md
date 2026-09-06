# MirvDesk Server

MirvDesk Server is the self-hosted backend for MirvDesk, an independent downstream project based on RustDesk.
It packages the RustDesk OSS rendezvous server (`hbbs`), relay server (`hbbr`), and the MirvDesk API in a single container.

## Goals

- one image and one Portainer stack;
- one persistent `/data` volume;
- direct RustDesk transport ports, with the HTTP API behind an existing nginx reverse proxy;
- account login, address books, groups, and synchronization without RustDesk Server Pro;
- stay close to the open RustDesk client wherever practical.

## Quick start

```bash
docker compose up -d
```

The recommended Linux deployment uses `network_mode: host`.
Open TCP 21115, TCP+UDP 21116 and TCP 21117 to clients. The API listens on `127.0.0.1:21114` and is intended to be published through nginx HTTPS.

See [Portainer + nginx](docs/portainer-nginx.md).
## Administrator utility

Create the first administrator locally inside the running container:

```bash
docker exec -it mirvdesk-server mirvdesk-admin
```

On an empty database the utility immediately asks for the login, optional display name, and password twice. Once users exist, the same command opens the administration menu. Password input is hidden and is never passed through command-line arguments or environment variables.

The same utility can reset a forgotten password for any existing user:

```bash
docker exec -it mirvdesk-server mirvdesk-admin passwd USERNAME
```

A password reset revokes all existing sessions for that user. `mirvdesk-admin list` lists the known accounts. The administrator utility works directly against `/data/mirvdesk.db`; there is no public HTTP bootstrap endpoint.

## API available now

- `GET /healthz`
- `GET /api/version`
- `GET /.well-known/mirvdesk`
- `GET /api/login-options`
- `POST /api/login`
- `POST /api/currentUser`
- `POST /api/logout`
- `GET /api/ab`
- `POST /api/ab`
## Security notes

Passwords are stored with Argon2id. Session tokens are random 256-bit values; only their SHA-256 hashes are stored server-side. Sessions currently expire after 30 days.

## Current status

The single-container runtime, RustDesk Server OSS 1.1.16 integration, local administrator utility, account login, current-user lookup, logout, runtime discovery, and personal address-book synchronization are implemented. Shared address books and groups are next.

## Runtime discovery

MirvDesk clients are generic builds: no ID server, relay server, API URL, or server public key is baked into CI. Enter the public MirvDesk server URL in the client and it fetches `/.well-known/mirvdesk`.

Behind nginx, the server derives its public URL from `Host` and `X-Forwarded-Proto`. `MIRVDESK_PUBLIC_URL`, `MIRVDESK_ID_SERVER`, `MIRVDESK_RELAY_SERVER`, and `MIRVDESK_API_PUBLIC_URL` are optional overrides for non-standard deployments.

## Upstream and license

RustDesk Server OSS is developed at https://github.com/rustdesk/rustdesk-server and is distributed under AGPL-3.0.
MirvDesk Server is independent from and is not endorsed by RustDesk/Purslane.
