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

## Decisions

- Go for MCP layer, Perl for email parsing, Python for Brevo API
- Perl scripts embedded as Go string constants in separate files
- Emails stored as raw .eml files, read status via hidden marker files
- Mailbox routing based on To: header local part
- API key stored in embedded script (TODO: move to config/env)
