# mail-mcp — Matrica Mail System

Go MCP server for the Matrica Mail System. Receives, stores, reads and sends
email for AI agents.

## Architecture

```
Sender → Cloudflare Email Routing → Worker
  → POST https://a-mail.matrica.work/mail/inbox
  → mail-server.pl (port 8901)
  → /srv/mail/{mailbox}/*.eml
  → mail-mcp (MCP server) → MCP tools
```

Outbound mail goes through Brevo SMTP API.

## MCP Tools

| Tool | Description |
|------|-------------|
| `mail_list` | List emails in a mailbox (paginated, unread filter) |
| `mail_read` | Read a full email by filename, mark as read |
| `mail_send` | Send an email via Brevo SMTP |
| `mail_attachment_get` | Retrieve an attachment from a received email |

## Quick Start

```bash
# Start the MCP server (stdin/stdout mode)
mail-mcp serve

# In opencode.json — register as remote MCP:
# {
#   "type": "remote",
#   "url": "http://localhost:7711/mail-mcp",
#   "timeout": 30000
# }
```

## Dependencies

- Go 1.24+
- Perl with Email::MIME
- Python 3 (for Brevo API)

## Files

- `main.go` — MCP server, tool definitions and handlers
- `parse.go` — embedded Perl Email::MIME parser script
- `send.go` — embedded Python Brevo API client
- `mail-server.pl` — inbox HTTP server (separate process)

## Storage

Emails stored in `/srv/mail/{mailbox}/*.eml` (raw RFC 822 format).
Read status tracked via hidden `.<file>.read` marker files.

## License

Copyright 2026 Kirill Scherba. All rights reserved.
BSD-style license.
