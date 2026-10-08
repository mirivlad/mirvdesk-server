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
