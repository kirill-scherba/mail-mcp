# Deploy — mail-mcp components

## Components

| File | Purpose |
| ---- | ------- |
| `mail-server.service` | systemd user unit for mail inbox server (port 8901) |
| `mail-server.pl` | Perl HTTP server, receives email from Cloudflare Worker |
| `a-mail.matrica.work.conf` | nginx config for a-mail.matrica.work domain |
| `mcp-gateway.service` | systemd user unit for MCP gateway (port 7711) |

## Deployment

```bash
# Copy service files
cp mail-server.service ~/.config/systemd/user/
cp mcp-gateway.service ~/.config/systemd/user/
systemctl --user daemon-reload

# Enable and start
systemctl --user enable --now mail-server
systemctl --user enable --now mcp-gateway

# Nginx config
cp a-mail.matrica.work.conf /etc/nginx/conf.d/
nginx -t && nginx -s reload
```

## Components (continued)

| File | Purpose |
| ---- | ------- |
| `worker.js` | Cloudflare Worker code (Email Routing → our server) |
| `mail-server.pl` | Perl HTTP server, receives email from Worker |

## Cloudflare Worker

**Deployed at:** `calm-truth-0d5b` in Cloudflare Dashboard → Workers & Pages

**Triggers:** Email Routing → matrica.work → catch-all → Worker

**Flow:**

```txt
Email → Cloudflare MX → Email Routing → Worker
  → POST https://a-mail.matrica.work/mail/inbox → mail-server.pl
```

The Worker receives the raw email from Cloudflare Email Routing and forwards
it as an HTTP POST to our server. Code in `worker.js`.

## Brevo SMTP (outbound)

Outbound email is sent through Brevo SMTP relay (`smtp-relay.brevo.com:587`).
Credentials stored in `send.go` (embedded in mail-mcp binary):

- **SMTP server:** smtp-relay.brevo.com:587 (STARTTLS)
- **Login:** <b35324001@smtp-brevo.com>
- **Sender:** Барон <baron@matrica.work>

Brevo domain verification: TXT records for matrica.work (SPF, DKIM) are
configured in Cloudflare DNS.

## Ports

- 8901 — mail-server.pl (inbox, HTTP)
- 7711 — mcp-gateway (MCP proxy)
