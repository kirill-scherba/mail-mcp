# Status — mail-mcp

## Done

- [x] MCP server loop (JSON-RPC stdin/stdout)
- [x] Tool: mail_list (paginated, unread filter)
- [x] Tool: mail_read (Perl Email::MIME parse, mark as read)
- [x] Tool: mail_send (Brevo API)
- [x] mail-server.pl (inbox HTTP server on port 8901)
- [x] Mailbox routing by recipient (baron, merchant, etc.)
- [x] Cloudflare Email Routing → Worker → a-mail.matrica.work
- [x] Let's Encrypt SSL for a-mail.matrica.work
- [x] Registered in mcp-gateway and opencode
- [x] Perl scripts extracted to parse.go as constants
- [x] Perl SMTP script extracted to send.go as constants
- [x] README.md with docs
- [x] Memory Bank (CONTEXT.md, STATUS.md, DESIGN.md)
- [x] Copyright headers and function comments
- [x] Fix email threading: set References/In-Reply-To on replies (#1)
- [x] Write conversation summaries via contact.summary after every non-trivial email (#2)

## In progress

- (none)

## Known issues

- API key hardcoded in send.go (should be env/config)
- Email subjects in mail_list are raw (base64 encoded) — mail_read decodes them
- No attachment handling in parser
- No spam/security checks on incoming mail
