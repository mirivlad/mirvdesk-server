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

Regular users and device groups are managed locally as well:

```bash
docker exec -it mirvdesk-server mirvdesk-admin create-user
docker exec -it mirvdesk-server mirvdesk-admin group-create Operations
docker exec -it mirvdesk-server mirvdesk-admin devices
docker exec -it mirvdesk-server mirvdesk-admin device-group 123456789 Operations
docker exec -it mirvdesk-server mirvdesk-admin group-add-user Operations alice
```

A device is registered automatically after its owner logs in from that MirvDesk client. `devices` shows known peer IDs, owners and current group assignment. Use `device-group PEER_ID none` to clear an assignment and `group-remove-user GROUP LOGIN` to revoke access. Administrators can see all registered devices; regular users see their own devices plus devices in groups granted to them.

The 1.6 group storage is a baseline and currently assigns at most one group to a device. The next administration milestone migrates device/group membership to many-to-many, so a device may belong to several groups and a user may see several groups. This transitional schema is not intended to become a permanent API contract.

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
- `GET /api/device-group/accessible`
- `GET /api/users`
- `GET /api/peers`

The last three endpoints implement the RustDesk-compatible Accessible devices / Groups view used by MirvDesk 1.6+.

## Security notes

Passwords are stored with Argon2id. Session tokens are random 256-bit values; only their SHA-256 hashes are stored server-side. Sessions currently expire after 30 days.

## Current status

The single-container runtime, RustDesk Server OSS 1.1.16 integration, local administrator utility, account login, current-user lookup, logout, runtime discovery, personal address-book synchronization, registered devices and Accessible devices / Groups are implemented. Shared address books remain future work.

## Runtime discovery

MirvDesk clients embed only the base URL of the self-hosted MirvDesk Server selected by the person compiling them. The build requires `MIRVDESK_SERVER_URL`; there is deliberately no project-wide public/default server. On first launch the client fetches `/.well-known/mirvdesk` from that URL and receives the ID server, relay server, API URL, public key and supported capabilities.

Behind nginx, the server derives its public URL from `Host` and `X-Forwarded-Proto`. `MIRVDESK_PUBLIC_URL`, `MIRVDESK_ID_SERVER`, `MIRVDESK_RELAY_SERVER`, and `MIRVDESK_API_PUBLIC_URL` are optional overrides for non-standard deployments.

## Roadmap and upstream compatibility

The cross-repository roadmap is maintained in the client repository: [MirvDesk roadmap](https://github.com/mirivlad/mirvdesk-client/blob/main/docs/ROADMAP.md). The planned primary administration UI is an authenticated window inside the MirvDesk desktop client, backed by a server-side admin API. A web admin UI, if added later, should reuse that same API rather than duplicate business logic.

MirvDesk-specific management endpoints should be namespaced so they do not unnecessarily collide with future RustDesk APIs. Existing RustDesk-compatible endpoints used by Accessible devices / Groups should retain their compatible shapes where practical. See the client-side [upstream compatibility policy](https://github.com/mirivlad/mirvdesk-client/blob/main/docs/UPSTREAM_COMPATIBILITY.md).

## Upstream and license

RustDesk Server OSS is developed at https://github.com/rustdesk/rustdesk-server and is distributed under AGPL-3.0.
MirvDesk Server is independent from and is not endorsed by RustDesk/Purslane.


### Device identity and accounts (1.7 preview)

A rendezvous ID identifies one remote host, not a MirvDesk API account.
Multiple API accounts may sign in from the same host and each receives a
separate address book. The first account remains the legacy display owner
of the device row; `user_devices` stores additional account relationships.
Signing in with another account does not replace the original device
metadata or modify its groups. Existing device records are migrated.

Sessions of disabled accounts are rejected. Address-book groups only
control visibility: **they do not grant or restrict RustDesk transport
connections**, whose password/approval checks remain independent.
A reported ID is not proof of possession of the remote host.
