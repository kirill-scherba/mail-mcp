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
- 2026-08-04: Add mail_attachment_get MCP tool (#3) — retrieves already-extracted attachments to a safe temp path
- 2026-10-17: Add Google Calendar support (#7) — calendar_list/get/create/update/delete tools reuse the Gmail OAuth flow
- 2026-11-21: Calendar for screens: `structuredContent` in all calendar tools, `calendar_calendars`
  (calendarList), recurring events expanded (`singleEvents=true`, `orderBy=startTime`), `max_results`
  up to 2500 read in pages of 250, all-day events by a date pair. A PATCH that changes an event from a
  timestamp to a date must null the old field, otherwise Calendar answers "Invalid start time"
  (checked live). The calendar module of AnyText is the client that asked for all of this.

## Decisions

- Go for MCP layer, Perl for email parsing, Perl for Brevo SMTP sending
- Perl scripts embedded as Go string constants in separate files
- Emails stored as raw .eml files, read status via hidden marker files
- Mailbox routing based on To: header local part
- API key in ~/.config/mcp-gateway/env, loaded via systemd EnvironmentFile
- Calendar is integrated into `mail-mcp` (not a separate server); it reuses the same Google OAuth token store and refresh logic
- Calendar uses raw HTTP against `www.googleapis.com/calendar/v3`, no `google.golang.org/api` dependency
- First run with Calendar requires `gmail_auth` re-authorization to obtain the calendar scope
- The text answer stays the agent's answer; the structured answer is the screen's. One handler fills
  both from the same event value, so the two cannot drift
