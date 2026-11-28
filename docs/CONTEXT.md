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

## 2026-11-23 — passive mail: one installation takes the letters of another

An installation without a Cloudflare route cannot own a mailbox: the route lives on the domain, and the
receiver lived inside `mail-mcp serve`. The receiver is now a service of its own and the letters travel
between installations on request.

- **`mail-mcp intake`** — the receiver alone: the port, the mailboxes and (with `MAIL_MCP_POLL=on`) the
  processing of the letters that belong to this installation. No MCP, no Google callback. Deployed as
  `mail-intake.service` with `~/.config/teonet/anytext/mail-intake.env`.
- **`MAIL_BOXES="address:handler;…"`** — the mailbox and the agent assigned to it. An empty handler makes
  the mailbox **transit**: the letters wait for the installation they belong to, no agent runs, and
  `mail_process` refuses the mailbox even when asked by hand. `MAIL_AGENTS` still works.
- **`POST /mail/fetch`**, **`POST /mail/ack`** — the passive half. `MAIL_FETCH_TOKENS` holds
  `token:address,…` pairs, so a token names the mailboxes it may serve. A letter leaves the source only
  after the confirmation and moves to `_delivered/`. nginx exposes both paths next to `/mail/inbox`.
- **`mail-mcp fetch`** — the passive client: a loop without MCP and without a port, run by a scheduler or a
  container entrypoint.
- A mailbox folder is an address (`pro_tools@matrica.work`); old bare folders (`baron`) are still found —
  the whole address is tried first, then its local part.

Three faults found by sending real letters, all fixed:

- the first letter with an attachment in a fresh mailbox lost the file: the rename into `attachments/`
  assumed that folder existed — this affected Cloudflare deliveries too;
- two letters of one batch arrive in the same second from one process and the second overwrote the first:
  the file name now carries nanoseconds;
- attachments were unpacked by a Perl one-liner needing `Email::MIME`, which a container does not have:
  they are unpacked in Go (`attachments.go`), and Perl stays as the fallback for exotic encodings.

Checked live on 23.11.2026: a letter posted to `https://a-mail.matrica.work/mail/inbox` (and later a real
letter through Cloudflare, DKIM and SPF passing) landed in `/srv/mail/pro_tools@matrica.work/`, the
installation `pro-tools` took it with `/mail/fetch` in 15–45 s, confirmed with `/mail/ack`, and its CSV
price file was unpacked inside the container.

Known gap: in the container the fetch loop is started by hand (`docker exec -d`), so it does not survive
the recreation of the container. The permanent home is one line in the image entrypoint, or the `pro`
module taking the letters itself — it is the one that needs the price files.

## 2026-11-28 — real price letters, and what they broke

Eighteen letters from `pro-tools.ru` (Sergey forwarding supplier lists) came through Cloudflare between
24 and 26 November: 46 files, from АВТОРУСЬ, Оптимист, Kimotozip, ЗИТРЕК РУС, ВЕРТЕКС, ТМК, MAXPILER,
STALEX, Интерскол, Аккорд, Уралоптинструмент, ИП Постников, Эврика, LIGE, JTC+GTE. They exposed three
faults that no synthetic test had found.

- **The receiver damaged letters.** It decoded the whole message as quoted-printable; the encoding belongs
  to the parts. The pass ate the `=` that ends an encoded word at a line break, merged `Subject` with
  `MIME-Version`, and turned `t=1795528205` into control characters. Now a message that parses is stored
  exactly as it arrived, and only an unparseable body gets the quoted-printable chance.
- **File names were unreadable.** Suppliers write a long name as an encoded word folded over two lines.
  `mime.ParseMediaType` stops at the fold and answers the tail; `mime.WordDecoder.DecodeHeader` leaves two
  words inside quotes undecoded. The header is unfolded, repaired where the receiver ate a character, and
  decoded word by word; the whitespace between two words is dropped.
- **A part was thrown away whole.** One incomplete base64 quantum at the end made the stream decoder fail
  and the partial data was dropped, so the letter counted as having no files. The text is collected,
  whitespace dropped, padding restored, and the bytes before the break are kept.

`mail-mcp attachments <mailbox>` unpacks a mailbox again — needed because letters already on disk keep the
names an older extractor gave them. Checked: 46 files, readable names, extensions kept, xlsx and zip open.

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
- 2026-11-21 (later): calendar_update takes pointer fields, so `""` clears a field and `"attendees": []`
  clears the list: a screen that lets a person empty a description must be able to empty it. Checked
  live: description and location cleared on a real event.
