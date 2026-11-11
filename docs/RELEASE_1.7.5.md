# MirvDesk Server 1.7.5

## Fixes

- Add an accessible **Show / Hide password** control on the web administrator login page.
- Add the same control in administrator dialogs for new user creation and password resets.
- Toggling visibility never changes the entered password or sends a request.
- Return the login field to masked mode when a session ends or the login page reappears.

## Updating

Image: `ghcr.io/mirivlad/mirvdesk-server:1.7.5`

For existing Portainer stacks, re-pull the image and redeploy, retaining the
current persistent `/data` volume and all network/reverse-proxy settings.
The web panel remains available at `https://YOUR-API-HOST/admin/`.

No database migration or desktop client update is required for this server-side fix.
