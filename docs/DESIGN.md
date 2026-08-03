# Design — mail-mcp

## Architecture

```
┌─────────────┐    ┌──────────────────┐    ┌──────────────┐
│ OpenCode     │◄──►│  mcp-gateway     │◄──►│  mail-mcp    │
│ (MCP client) │    │  localhost:7711  │    │  (Go MCP)    │
└─────────────┘    └──────────────────┘    └──────┬───────┘
                                                  │
                                  ┌───────────────┴───────────────┐
                                  │  parse.go (Perl Email::MIME)   │
                                  │  send.go (Perl Brevo SMTP)     │
                                  └───────────────────────────────┘
```

## Data flow

### Incoming email
```
SMTP → Cloudflare MX → Email Routing → Worker
  → POST /mail/inbox → mail-server.pl → /srv/mail/{mailbox}/*.eml
  → mail-mcp reads via mail_list / mail_read
```

### Outgoing email
```
Agent → mail_send tool → Perl SMTP script → Brevo SMTP relay → Recipient

Reply flow:
process.go extracts Message-ID from incoming email
  → sendSMTP passes it to the Perl script
  → Perl adds In-Reply-To and References headers
  → Brevo SMTP relay delivers the threaded reply
```

## Storage format

- Raw email (RFC 822) stored as .eml files
- One file per email, named `YYYYMMDD_HHMMSS_PID.eml`
- Read status: hidden `.<file>.read` marker file
- No database needed for MVP — filesystem is the store

## Mailbox routing

When mail-server.pl receives an email, it parses the To: header.
The local part before @matrica.work determines the mailbox:
- `baron@matrica.work` → /srv/mail/baron/
- `merchant@matrica.work` → /srv/mail/merchant/

Unknown mailboxes fall back to /srv/mail/inbox/.

## Security

- Inbound email goes through Cloudflare Email Routing with SPF/DKIM/DMARC checks
- Outbound signed with Brevo DKIM
- No PGP yet (planned)
- No spam filtering yet (planned: LLM-based classification)

## Dependencies

- Cloudflare Email Routing (inbound MX)
- Cloudflare Worker (email → HTTP bridge)
- Let's Encrypt (SSL for a-mail.matrica.work)
- Brevo API (outbound SMTP)
- Perl Email::MIME (email parsing)
