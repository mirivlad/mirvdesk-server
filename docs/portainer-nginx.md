# Portainer + nginx

MirvDesk Server is designed to run as one container with host networking on Linux.
This lets `hbbs` and `hbbr` see the real client IP and lets a host nginx proxy reach the API on loopback.

## Portainer

Create a new Stack and paste `compose.yml`. The stack uses one named volume, `mirvdesk-data`.
Open TCP 21115, TCP+UDP 21116, and TCP 21117 in the host firewall/NAT. Do not proxy these ports through nginx.

After the first start, open the `mirvdesk-server` container console in Portainer and run:

```bash
mirvdesk-admin
```

Use the same utility later to reset a forgotten account password.

## nginx API proxy

The API binds to `127.0.0.1:21114` by default, so it is not directly exposed to the Internet.
Point a normal HTTPS virtual host at it:

```nginx
server {
    listen 443 ssl http2;
    server_name api.example.com;

    location / {
        proxy_pass http://127.0.0.1:21114;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Use your existing certificate/Certbot setup for TLS. In MirvDesk Client enter `https://api.example.com` as the MirvDesk Server address; runtime discovery fills in the ID server, relay server, API URL, and public key automatically.
