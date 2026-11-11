# MirvDesk 1.7.3 — automatic device inventory (development)

## Registry and presence

The MirvDesk Server imports rendezvous peer IDs and public keys read-only from /data/rustdesk/db_v2.sqlite3 every 30 seconds. A device appears in the administrator list without MirvDesk account login, initially without an owner or group.

Only a host MirvDesk client service sends signed heartbeats (every 60 seconds). It obtains a one-time challenge valid for 60 seconds and signs ID, nonce, hostname, OS, architecture, and client version with the existing RustDesk Ed25519 private key. The server verifies against the pinned HBBS public key. A changed key cannot silently take over an existing device.

- online: verified heartbeat within 180 seconds
- offline: last verified heartbeat is older than 180 seconds
- unknown: no verified heartbeat yet, even if a row exists in HBBS

An HBBS database record is not live-presence evidence. Old RustDesk clients may appear with unknown presence until they support the MirvDesk heartbeat protocol.

## API

- GET /api/device-registry/challenge?id=PEER_ID (public challenge, no account login)
- POST /api/device-registry/heartbeat (signed presence only)
- GET /api/admin/devices?scope=all|mine|accessible (admin bearer token required)
- PUT /api/admin/devices/{peer}/display-name with JSON name (admin only)

The Ed25519 signature covers UTF-8 lines separated by LF: mirvdesk-heartbeat-v1, ID, nonce, hostname, OS, architecture, and version, in that order. The signature is standard base64 encoded.

## Security boundaries

Discovery does not grant account ownership, group membership, unattended access, or remote control rights. Device groups still control address-list visibility rather than HBBS transport authorization. Host-reported metadata is trusted only after cryptographic proof. Device alias and administrator note are stored separately from reported hostname.

## Portainer upgrade

The existing /data volume is sufficient. No additional open port or volume is needed. Back up the live SQLite database consistently (including WAL or via SQLite backup API) before deploying. On upgrade, the devices.owner_user_id column is migrated to allow NULL, preserving old peer IDs, group assignments and notes. If HBBS peer key material changes unexpectedly, the server rejects a silent rotation.

Do not deploy this development branch to production before native Windows/Linux/macOS builds, cross-platform tests and an end-to-end smoke test.

## Test coverage

Go tests cover discovery without account login, signed heartbeats, replay and metadata modification, key rotation rejection, presence expiration, alias changes, group permissions, legacy migration, and anonymous admin isolation.