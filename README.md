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
## First administrator

On a fresh `/data` volume MirvDesk Server creates `/data/bootstrap.token` (mode `0600`) and prints the same one-time token to the container log.
Use it once to create the first administrator:

```bash
curl -X POST https://api.example.com/api/bootstrap \
  -H 'Content-Type: application/json' \
  -d '{"token":"TOKEN","username":"admin","password":"change-this-password","display_name":"Administrator"}'
```

After the first administrator is created the bootstrap token file is deleted and the endpoint cannot create another administrator.
A MirvDesk client bootstrap UI is planned so this curl step can disappear.

## API available now

- `GET /healthz`
- `GET /api/version`
- `GET /api/login-options`
- `GET /api/bootstrap/status`
- `POST /api/bootstrap`
- `POST /api/login`
- `POST /api/currentUser`
- `POST /api/logout`
## Security notes

Passwords are stored with Argon2id. Session tokens are random 256-bit values; only their SHA-256 hashes are stored server-side. Sessions currently expire after 30 days.

## Current status

The single-container runtime, RustDesk Server OSS 1.1.16 integration, bootstrap, account login, current-user lookup and logout are implemented. Address-book and group endpoints are next.

## Upstream and license

RustDesk Server OSS is developed at https://github.com/rustdesk/rustdesk-server and is distributed under AGPL-3.0.
MirvDesk Server is independent from and is not endorsed by RustDesk/Purslane.
