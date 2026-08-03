# Context — mail-mcp

## Current state

mail-mcp is a new MCP server providing email tools for AI agents in the Matrica
ecosystem. It is fully functional: receive, list, read, and send email.

## Recent changes

- 2026-07-26: Created mail-mcp project, implemented MCP protocol layer
- 2026-07-26: Basic tools (mail_list, mail_read, mail_send) working
- 2026-07-26: Perl Email::MIME parser integrated
- 2026-07-26: Brevo SMTP send integrated
- 2026-07-26: Connected to mcp-gateway and opencode
- 2026-08-03: Fix email threading (#1) — replies now carry In-Reply-To and References headers

## Decisions

- Go for MCP layer, Perl for email parsing, Perl for Brevo SMTP sending
- Perl scripts embedded as Go string constants in separate files
- Emails stored as raw .eml files, read status via hidden marker files
- Mailbox routing based on To: header local part
- API key in ~/.config/mcp-gateway/env, loaded via systemd EnvironmentFile
