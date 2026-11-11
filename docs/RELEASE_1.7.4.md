# MirvDesk Server 1.7.4

## Highlights

- Built-in web administration panel at /admin/ (same container/API origin).
- Login with an existing MirvDesk administrator account; no public bootstrap.
- User creation, status management and password resets.
- Device inventory with presence metadata, display names, administrator notes,
  and multiple group assignments.
- Group creation, rename, removal and group member management.
- Overview of hbbs/hbbr health, version information and administrator audit log.
- Responsive Russian-language UI, strict CSP/security headers and bundled
  static assets with no external CDN or dependencies.

This release also contains the server-side changes developed after 1.7.1,
including the signed HBBS device registry/heartbeat groundwork.

## Deployment

Image: ghcr.io/mirivlad/mirvdesk-server:1.7.4

Use the existing Portainer stack with the same /data volume. Update the image
and recreate the container; do **not** remove the persistent volume.
The API and web panel share the same HTTPS reverse proxy.

Panel URL: https://YOUR-API-HOST/admin/

Read docs/WEB_ADMIN.md before the update.

## Important

Device groups control visibility in the address book. They **do not**
authorize unattended desktop access or bypass RustDesk transport
authentication. The initial administrator and administrator privilege
elevation remain CLI-only.
