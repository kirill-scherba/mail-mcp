# Deploy — mail-mcp components

## Components

| File | Purpose |
|------|---------|
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

## Ports

- 8901 — mail-server.pl (inbox)
- 7711 — mcp-gateway (MCP proxy)
