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
                                   │  gmail.go + calendar.go        │
                                   │  (Google OAuth / raw HTTP)     │
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
- Attachments extracted on receive to `/srv/mail/{mailbox}/attachments/{eml}/{attachment}`
- No database needed for MVP — filesystem is the store

## Attachment retrieval

`mail_attachment_get` reads already-extracted attachments from the filesystem store above,
copies them to a safe temporary path (e.g. under `/tmp`), and returns JSON metadata
(`path`, `size`, `content_type`). Path components (`mailbox`, `file`, `attachment`) are
validated to prevent directory traversal. Attachments are written with mode `0600` and
exclusive creation (`O_EXCL`) to avoid overwriting existing files.

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

## Calendar

Google Calendar is integrated in `calendar.go` using raw HTTP against
`https://www.googleapis.com/calendar/v3`, just as Gmail uses raw HTTP against
the Gmail API. Both share the same OAuth2 config, token store, and refresh
logic from `gmail.go`. Calendar support adds one scope
(`https://www.googleapis.com/auth/calendar`) to the existing consent flow, so
the first deployment requires re-authorization through `gmail_auth`.

Calendar events are returned as compact human-readable text. Recurring events
are returned as masters with their raw `recurrence` rule; occurrence expansion
is not performed in this step.

## Dependencies

- Cloudflare Email Routing (inbound MX)
- Cloudflare Worker (email → HTTP bridge)
- Let's Encrypt (SSL for a-mail.matrica.work)
- Brevo API (outbound SMTP)
- Google Gmail API (read/send)
- Google Calendar API (events CRUD)
- Perl Email::MIME (email parsing)
