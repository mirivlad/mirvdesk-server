# Administrator API (1.7 development)

All endpoints require a valid Bearer token for an administrator account.
Non-admin users receive HTTP 403, unauthenticated callers HTTP 401.

- GET /api/admin/users?current=1&pageSize=100
- GET /api/admin/groups?current=1&pageSize=100
- POST /api/admin/groups with JSON {"name":"Operations"}
- GET /api/admin/devices?current=1&pageSize=100
- PUT /api/admin/devices/{peer}/group with JSON {"group":"Operations"} (or "none")
- POST /api/admin/groups/{group}/members with JSON {"username":"alice"}
- DELETE /api/admin/groups/{group}/members/{username}

The discovery endpoint advertises admin-api to clients.

This API manages **visibility** in the MirvDesk groups panel. It does not
enforce transport-layer access to a remote desktop. The server must not claim
that sharing/group membership is an hbbs authorization policy.

This API is a prerequisite for administrator screens in the desktop client.
It is not included in the 1.6.1 release.


### Multiple device groups (compatible with 1.6.x)

- GET /api/admin/devices/{peer}/groups returns {"groups":["Operations","Support"]}.
- PUT /api/admin/devices/{peer}/groups with JSON {"groups":["Operations","Support"]} atomically replaces all assignments. Empty array removes all assignments.

The existing singular group endpoint replaces the full set and keeps the
primary group value for clients 1.6.x. List responses now include
device_group_names (array) in addition to legacy device_group_name.
The upgrade migrates pre-existing single-group assignments idempotently.

### Password guessing mitigation

The login endpoint keeps a bounded in-memory failure counter. Eight bad
passwords for an account and source address, or eighty bad passwords from
one direct source address across usernames, cause HTTP 429 and Retry-After for
approximately two minutes. Counters are shared within one running API
instance, and reset when the process restarts. The local nginx reverse proxy is a shared 127.0.0.1 hop: the IP-wide limit
is intentionally disabled for loopback sources to avoid locking out every
user at once; username+proxy limits remain. Forwarded-IP headers are never
trusted without an explicit trusted-proxy configuration.


### Administrator audit events

GET /api/admin/audit returns most recent administrator actions (up to 1,000
retained in query), including actor, action, resource ID and timestamp.
Credentials, session tokens and passwords are never part of these events.
Records are stored in the persistent SQLite database; this is a basic
operational log, not a tamper-proof security audit system.


### Administrator user lifecycle

- POST /api/admin/users with JSON {"username":"alice","display_name":"Alice","password":"at-least-10-chars"} creates a **non-admin** account. Only the local CLI can elevate or create administrators.
- PATCH /api/admin/users/{username}/status with JSON {"enabled":false} disables an account, revokes existing sessions and prevents login. Setting true enables login but does not restore old sessions.
- PUT /api/admin/users/{username}/password with JSON {"password":"new-strong-password"} resets credentials, invalidating every current session for that user.

An administrator cannot disable their own account or the last active administrator. All mutations require server-validated administrator sessions and are logged with no password contents.

### Group rename and removal

- PUT /api/admin/groups/{group} with JSON {"name":"New name"} renames a group while retaining its member/device links (HTTP 409 on duplicate name).
- DELETE /api/admin/groups/{group} deletes the group and cascading membership/device links; other device groups remain intact. The legacy primary group FK is cleared by SQLite.

### Device metadata

PUT /api/admin/devices/{peer}/note with JSON {"note":"Reception workstation"}
stores a manager note (up to 1000 characters). Device lists include the
last_account_login Unix timestamp: this is the last successful account
registration from that client, **not live device presence**, and should never
be represented by a green online badge without a real hbbs/presence check.
