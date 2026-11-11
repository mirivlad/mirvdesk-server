# MirvDesk Web Administration (1.7.4)

MirvDesk includes a built-in web administration interface. It is served by the
**existing HTTP API process** within the **same container** at:

    https://YOUR-API-HOST/admin/

There is no separate frontend container, public admin port, or extra database.

## Requirements

- MirvDesk Server 1.7.4 or newer.
- A working HTTPS reverse proxy forwarding the full URL path to the HTTP API on
  127.0.0.1:21114 (see [Portainer + nginx](portainer-nginx.md)).
- An existing administrator account. For the first admin, execute this inside
  the container:

    docker exec -it mirvdesk-server mirvdesk-admin

Use the same credentials as the MirvDesk account. Non-admin accounts cannot
use the panel. The initial administrator must be created from the local CLI;
the web login deliberately does not offer public bootstrap or self-registration.

## Features

- Overview: user, device and group counts, server health, recent admin events.
- Users: create a regular user, enable/disable accounts, reset passwords.
- Devices: browse signed HBBS registry, set a display name, notes and multiple
  group assignments. Heartbeat presence is shown independently from account
  login timestamps.
- Groups: create/rename/delete groups; assign/remove group members.
- Audit: view administrative events.
- Server: view versions and health of hbbs / hbbr / API.

**Important:** Groups govern which devices are *visible* to an account. They
do **not** bypass or replace RustDesk's transport authentication, unattended
access passwords, or connection approval.

The interface uses the existing administrator Bearer-token API with server-side
permission checks. Tokens are kept in browser sessionStorage (per tab, never
localStorage), are not sent to unrelated origins, and are cleared on logout.
Closing the tab clears the browser's session storage in most browsers, but
tokens remain valid server-side until revoked or expired. For revocation,
use logout or reset the account password. Use HTTPS.

A strict Content-Security-Policy, no-frame headers and no-store cache headers
are applied to the HTML, CSS and JavaScript. Static assets are embedded in
the Go executable. They cannot be edited from the persistent data volume.

## Updating a Portainer stack

1. **Back up** the existing persistent /data volume, including
   mirvdesk.db and the rustdesk/ directory, while the service is stopped
   or by a consistent snapshot.
2. In Portainer, open your existing MirvDesk Stack. Keep its same volumes,
   network mode, environment and ports. Change the image to
   ghcr.io/mirivlad/mirvdesk-server:1.7.4.
3. Use Portainer's Update the stack / re-pull image option. Do not delete
   the stack volume or create a new stack with a different named volume.
4. Check https://YOUR-API-HOST/healthz and https://YOUR-API-HOST/admin/.
5. Sign in as an existing administrator.

If the UI opens but API calls return 404, the reverse proxy may forward only
/api/* rather than the full /admin/* paths. Proxy both paths to the same
existing API upstream and preserve HTTPS/Host headers.

### nginx example

This assumes an existing TLS virtual host and the default loopback binding:

    location / {
        proxy_pass http://127.0.0.1:21114;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

### Recovery

- If the web session is no longer valid: sign in again.
- If the administrator password is lost:

    docker exec -it mirvdesk-server mirvdesk-admin passwd USERNAME

- If needed, roll back to the previous image while keeping a consistent
  backup of the database and the data volume. Do not remove /data.

**Limitations:** The server information page is read-only; modifying Docker,
TLS, network ports and runtime configuration is intentionally left to the
operator's container/reverse-proxy tooling. The admin UI does not grant
arbitrary remote-shell or file access.
