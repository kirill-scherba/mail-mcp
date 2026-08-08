package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MCP Tool definitions
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type ToolCall struct {
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// MailDir is where emails are stored
const MailDir = "/srv/mail"

// Default agent, mailbox, and sender identity
const defaultAgent = "baron"
const defaultMailbox = "baron"
const defaultFromName = "Барон"
const defaultFromAddr = "baron@matrica.work"

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 || os.Args[1] == "serve" {
		serveMCP()
		return
	}
	fmt.Println("mail-mcp MCP server. Usage: mail-mcp serve")
}

func serveMCP() {
	// Init Gmail OAuth and start callback server
	initGmailOAuth()
	startGmailCallbackServer()
	// Start inbox server (replaces mail-server.pl)
	startInboxServer()
	// Start periodic mail poller
	startMailPoller()

	// Read JSON-RPC from stdin
	decoder := json.NewDecoder(os.Stdin)
	var request struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		ID     int             `json:"id"`
	}

	for decoder.Decode(&request) == nil {
		switch request.Method {
		case "initialize":
			respond(request.ID, map[string]interface{}{
				"protocolVersion": "0.1.0",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
			})
		case "tools/list":
			respond(request.ID, map[string]interface{}{
				"tools": getTools(),
			})
		case "tools/call":
			var call ToolCall
			json.Unmarshal(request.Params, &call)
			result := handleToolCall(call)
			respond(request.ID, result)
		case "notifications/initialized":
			// ignore
		default:
			respondError(request.ID, -32601, "Method not found")
		}
	}
}

func respond(id int, result interface{}) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	json.NewEncoder(os.Stdout).Encode(response)
}

func respondError(id int, code int, message string) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	json.NewEncoder(os.Stdout).Encode(response)
}

func getTools() []Tool {
	return []Tool{
		{
			Name:        "mail_list",
			Description: "List emails in a mailbox. Returns paginated list with read/unread status.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"mailbox": {"type": "string", "description": "Mailbox name (e.g. baron, merchant)", "default": "baron"},
					"folder":  {"type": "string", "description": "Folder: inbox (default) or sent", "default": "inbox"},
					"page":    {"type": "integer", "description": "Page number (1-based)", "default": 1},
					"limit":   {"type": "integer", "description": "Items per page", "default": 10},
					"unread_only": {"type": "boolean", "description": "Show only unread", "default": false}
				}
			}`),
		},
		{
			Name:        "mail_read",
			Description: "Read a full email by filename. Decodes headers and body. Marks as read (inbox only).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"mailbox": {"type": "string", "description": "Mailbox name", "default": "baron"},
					"folder":  {"type": "string", "description": "Folder: inbox (default) or sent", "default": "inbox"},
					"file": {"type": "string", "description": "Email filename (e.g. 20260726_133524_4046661.eml)"}
				},
				"required": ["file"]
			}`),
		},
		{
			Name:        "mail_send",
			Description: "Send an email via Brevo SMTP. Optionally attach a file.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"to":      {"type": "string", "description": "Recipient email"},
					"subject": {"type": "string", "description": "Email subject"},
					"body":    {"type": "string", "description": "Email body text"},
					"attachment_path": {"type": "string", "description": "Path to file to attach (optional)"}
				},
				"required": ["to", "subject", "body"]
			}`),
		},
		{
			Name:        "mail_process",
			Description: "Запускает почтовую сессию: сканирует новые письма, создаёт opencode-сессию для каждого, отправляет ответы.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"agent": {"type": "string", "description": "Agent name (default: baron)", "default": "baron"}
				}
			}`),
		},
		{
			Name:        "contact_get",
			Description: "Get contact info from the address book by email.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"email": {"type": "string", "description": "Email address"}
				},
				"required": ["email"]
			}`),
		},
		{
			Name:        "contact_save",
			Description: "Create or update a contact in the address book.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"email": {"type": "string", "description": "Email address"},
					"name": {"type": "string", "description": "Display name"},
					"who": {"type": "string", "description": "Brief description"},
					"summary": {"type": "string", "description": "Dialogue summary"}
				},
				"required": ["email"]
			}`),
		},
		{
			Name:        "gmail_auth",
			Description: "Start Gmail OAuth2 authorization. Returns a URL to visit for granting access.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"account": {"type": "string", "description": "Gmail account (email) to authorize (default: GMAIL_DEFAULT_READ_ACCOUNT)"}
				}
			}`),
		},
		{
			Name:        "gmail_list",
			Description: "List recent messages from Gmail (requires gmail_auth first). Returns subjects, senders and dates.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {"type": "integer", "description": "Max results (default 10, max 50)"},
					"account": {"type": "string", "description": "Gmail account (email) to read (default: GMAIL_DEFAULT_READ_ACCOUNT)"}
				}
			}`),
		},
		{
			Name:        "gmail_send",
			Description: "Send an email via Gmail API. Optionally attach a file.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"to":      {"type": "string", "description": "Recipient email"},
					"subject": {"type": "string", "description": "Subject"},
					"body":    {"type": "string", "description": "Email body"},
					"attachment_path": {"type": "string", "description": "Path to file to attach (optional)"},
					"account": {"type": "string", "description": "Gmail account (email) to send from (default: GMAIL_DEFAULT_SEND_ACCOUNT)"}
				},
				"required": ["to", "subject", "body"]
			}`),
		},
		{
			Name:        "gmail_read",
			Description: "Read a full Gmail message by ID.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "Gmail message ID"},
					"account": {"type": "string", "description": "Gmail account (email) to read from (default: GMAIL_DEFAULT_READ_ACCOUNT)"}
				},
				"required": ["id"]
			}`),
		},
		{
			Name:        "gmail_search",
			Description: "Search Gmail messages by query and read them.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "Search query (e.g. 'from:bank subject:invoice')"},
					"limit": {"type": "integer", "description": "Max results (default 10)"},
					"account": {"type": "string", "description": "Gmail account (email) to search (default: GMAIL_DEFAULT_READ_ACCOUNT)"}
				},
				"required": ["query"]
			}`),
		},
		{
			Name:        "mail_attachment_get",
			Description: "Retrieve an attachment from a received email. Saves the file to a safe temporary path and returns JSON with path, size, and MIME type. Attachments must have been extracted on receive — call mail_read first to verify availability.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"mailbox":     {"type": "string", "description": "Mailbox name (e.g. baron, merchant)", "default": "baron"},
					"file":        {"type": "string", "description": "Email filename (e.g. 20260803_070000_12345.eml)"},
					"attachment":  {"type": "string", "description": "Attachment filename as shown in mail_read output"}
				},
				"required": ["file", "attachment"]
			}`),
		},
	}
}

func handleToolCall(call ToolCall) ToolResult {
	switch call.Name {
	case "mail_list":
		return handleMailList(call.Arguments)
	case "mail_read":
		return handleMailRead(call.Arguments)
	case "mail_send":
		return handleMailSend(call.Arguments)
	case "mail_attachment_get":
		return handleMailAttachmentGet(call.Arguments)
	case "mail_process":
		var mp struct {
			Agent string `json:"agent"`
		}
		if len(call.Arguments) > 0 {
			json.Unmarshal(call.Arguments, &mp)
		}
		return handleMailProcess(&mp)
	case "contact_get":
		return handleContactGet(call.Arguments)
	case "contact_save":
		return handleContactSave(call.Arguments)
	case "gmail_auth":
		var p struct {
			Account string `json:"account"`
		}
		json.Unmarshal(call.Arguments, &p)
		return handleGmailAuth(p.Account)
	case "gmail_list":
		var p struct {
			Limit   int    `json:"limit"`
			Account string `json:"account"`
		}
		json.Unmarshal(call.Arguments, &p)
		return handleGmailList(p.Limit, p.Account)
	case "gmail_send":
		var p struct {
			To             string `json:"to"`
			Subject        string `json:"subject"`
			Body           string `json:"body"`
			AttachmentPath string `json:"attachment_path"`
			Account        string `json:"account"`
		}
		json.Unmarshal(call.Arguments, &p)
		return handleGmailSend(p.To, p.Subject, p.Body, p.AttachmentPath, p.Account)
	case "gmail_read":
		var p struct {
			ID      string `json:"id"`
			Account string `json:"account"`
		}
		json.Unmarshal(call.Arguments, &p)
		return handleGmailRead(p.ID, p.Account)
	case "gmail_search":
		var p struct {
			Query   string `json:"query"`
			Limit   int    `json:"limit"`
			Account string `json:"account"`
		}
		json.Unmarshal(call.Arguments, &p)
		return handleGmailSearch(p.Query, p.Limit, p.Account)
	default:
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Unknown tool: " + call.Name}}}
	}
}

func handleMailList(args json.RawMessage) ToolResult {
	var params struct {
		Mailbox    string `json:"mailbox"`
		Folder     string `json:"folder"`
		Page       int    `json:"page"`
		Limit      int    `json:"limit"`
		UnreadOnly bool   `json:"unread_only"`
	}
	json.Unmarshal(args, &params)
	if params.Mailbox == "" { params.Mailbox = defaultMailbox }
	if params.Page < 1 { params.Page = 1 }
	if params.Limit < 1 { params.Limit = 10 }

	folderPath := MailDir + "/" + params.Mailbox
	if params.Folder == "sent" {
		folderPath += "/sent"
	}
	mailboxDir := folderPath
	entries, err := os.ReadDir(mailboxDir)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Cannot read mailbox: " + err.Error()}}}
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".eml") {
			readFile := mailboxDir + "/." + e.Name() + ".read"
			if _, err := os.Stat(readFile); params.UnreadOnly && err == nil {
				continue
			}
			files = append(files, e.Name())
		}
	}

	// Simple sort: newest first (we reverse)
	total := len(files)
	start := (params.Page - 1) * params.Limit
	if start >= total {
		return ToolResult{Content: []ContentItem{{Type: "text", Text: "[]"}}}
	}
	end := start + params.Limit
	if end > total { end = total }

	var result []map[string]interface{}
	for i := 0; i < len(files); i++ {
		idx := total - 1 - i // newest first
		fname := files[idx]
		readFile := mailboxDir + "/." + fname + ".read"
		_, isRead := os.Stat(readFile)
		info, _ := os.Stat(mailboxDir + "/" + fname)

		entry := map[string]interface{}{
			"file":   fname,
			"size":   info.Size(),
			"unread": os.IsNotExist(isRead),
		}

		// Get subject quickly from first line
		data, _ := os.ReadFile(mailboxDir + "/" + fname)
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "Subject: ") {
				entry["subject"] = strings.TrimPrefix(line, "Subject: ")
			}
			if strings.HasPrefix(line, "From: ") {
				entry["from"] = strings.TrimPrefix(line, "From: ")
			}
			if strings.HasPrefix(line, "Date: ") {
				entry["date"] = strings.TrimPrefix(line, "Date: ")
			}
			if line == "" || line == "\r" {
				break
			}
		}

		result = append(result, entry)
	}

	// Paginate
	if start < len(result) {
		result = result[start:end]
	}

	out, _ := json.Marshal(result)
	return ToolResult{Content: []ContentItem{{Type: "text", Text: string(out)}}}
}

func handleMailRead(args json.RawMessage) ToolResult {
	var params struct {
		Mailbox string `json:"mailbox"`
		Folder  string `json:"folder"`
		File    string `json:"file"`
	}
	json.Unmarshal(args, &params)
	if params.Mailbox == "" { params.Mailbox = defaultMailbox }
	if params.Folder == "" { params.Folder = "inbox" }

	basePath := MailDir + "/" + params.Mailbox
	if params.Folder == "sent" {
		basePath += "/sent"
	}
	emailPath := basePath + "/" + params.File
	if _, err := os.Stat(emailPath); os.IsNotExist(err) {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Email not found"}}}
	}

	// Call Perl parser (constant from parse.go)
	cmd := exec.Command("perl", "-e", parsePerlScript)
	data, _ := os.ReadFile(emailPath)
	cmd.Stdin = strings.NewReader(string(data))
	out, err := cmd.Output()
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Parse error: " + err.Error()}}}
	}

	// Mark as read (only for inbox, sent are outgoing copies)
	if params.Folder != "sent" {
		readFile := MailDir + "/" + params.Mailbox + "/." + params.File + ".read"
		os.WriteFile(readFile, []byte{}, 0644)
	}

	// Check for extracted attachments and append to output
	raw, _ := os.ReadFile(emailPath)
	attachments := ensureAttachments(MailDir+"/"+params.Mailbox, params.File, raw)
	if len(attachments) > 0 {
		result := string(out) + "\n\n---\nВложения:\n"
		for _, name := range attachments {
			path := MailDir + "/" + params.Mailbox + "/attachments/" + params.File + "/" + name
			result += fmt.Sprintf("- %s — %s\n", name, path)
		}
		return ToolResult{Content: []ContentItem{{Type: "text", Text: result}}}
	}

	return ToolResult{Content: []ContentItem{{Type: "text", Text: string(out)}}}
}

func handleMailSend(args json.RawMessage) ToolResult {
	var params struct {
		To             string `json:"to"`
		Subject        string `json:"subject"`
		Body           string `json:"body"`
		AttachmentPath string `json:"attachment_path"`
	}
	json.Unmarshal(args, &params)

	if params.To == "" || params.Subject == "" || params.Body == "" {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Missing required fields"}}}
	}

	sendBody := params.Body
	if params.AttachmentPath != "" {
		attData, err := os.ReadFile(params.AttachmentPath)
		if err != nil {
			return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("attachment read error: %v", err)}}}
		}
		boundary := "matrica_boundary_123456"
		_, attName := filepath.Split(params.AttachmentPath)
		attB64raw := base64.StdEncoding.EncodeToString(attData)
		// Split base64 into 76-char lines
		var attB64 strings.Builder
		for i := 0; i < len(attB64raw); i += 76 {
			end := i + 76
			if end > len(attB64raw) { end = len(attB64raw) }
			attB64.WriteString(attB64raw[i:end] + "\r\n")
		}
		// Only the body after headers: boundary + text + boundary + attachment
		sendBody = fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\nMIME-Version: 1.0\r\n\r\n--%s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n--%s\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"%s\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n--%s--",
			boundary, boundary, params.Body,
			boundary, attName, attB64.String(), boundary)
	}

	// Send via Brevo SMTP (stdin to avoid ARG_MAX)
	// Empty inReplyTo: mail_send creates a brand-new email, not a reply.
	if err := sendSMTP(defaultFromName, defaultFromAddr, params.To, params.Subject, sendBody, ""); err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Send error: " + err.Error()}}}
	}

	// Save sent copy
	saveSentEmail(MailDir+"/"+defaultMailbox, params.To, params.Subject, params.Body)

	return ToolResult{Content: []ContentItem{{Type: "text", Text: "✅ Sent"}}}
}

// isSafePathElement reports whether name is a single, safe filesystem path element.
// It rejects separators, traversal sequences, and empty/null names.
func isSafePathElement(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, "/\\")
}

// sanitizeFilename returns a safe filename derived from s: only letters,
// digits, underscore, dash, and dot are preserved; everything else becomes '_'
// and the result is trimmed to a reasonable length.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 128 {
		out = out[:128]
	}
	if out == "" || out == "." || out == ".." {
		out = "attachment"
	}
	return out
}

// handleMailAttachmentGet retrieves an attachment from a received email.
// Attachments must have been extracted on receive via ensureAttachments().
// The file is copied to a safe temporary location and JSON metadata is returned.
func handleMailAttachmentGet(args json.RawMessage) ToolResult {
	var params struct {
		Mailbox    string `json:"mailbox"`
		File       string `json:"file"`
		Attachment string `json:"attachment"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{
			Type: "text", Text: "Invalid arguments: " + err.Error(),
		}}}
	}
	if params.Mailbox == "" {
		params.Mailbox = defaultMailbox
	}
	if !isValidMailbox(params.Mailbox) {
		return ToolResult{IsError: true, Content: []ContentItem{{
			Type: "text", Text: "Invalid mailbox name",
		}}}
	}
	if !isSafePathElement(params.File) || !isSafePathElement(params.Attachment) {
		return ToolResult{IsError: true, Content: []ContentItem{{
			Type: "text", Text: "Invalid file or attachment name",
		}}}
	}

	srcPath := filepath.Join(MailDir, params.Mailbox,
		"attachments", params.File, params.Attachment)

	data, err := os.ReadFile(srcPath)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{
			Type: "text", Text: fmt.Sprintf(
				"Attachment not found: %s (email: %s, attachment: %s). "+
					"Call mail_read first to verify the attachment exists.",
				srcPath, params.File, params.Attachment),
		}}}
	}

	safeName := sanitizeFilename(params.Attachment)
	destPath := filepath.Join(os.TempDir(),
		fmt.Sprintf("mail_attachment_%d_%s",
			time.Now().UnixNano(), safeName))

	// Write with restrictive permissions. A unique nanosecond timestamp makes
	// collisions practically impossible, but O_EXCL prevents overwriting
	// should one somehow occur.
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{
			Type: "text", Text: fmt.Sprintf("Write error: %v", err),
		}}}
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(destPath)
		return ToolResult{IsError: true, Content: []ContentItem{{
			Type: "text", Text: fmt.Sprintf("Write error: %v", errors.Join(writeErr, closeErr)),
		}}}
	}

	mimeType := mime.TypeByExtension(filepath.Ext(params.Attachment))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	result := map[string]interface{}{
		"path":         destPath,
		"size":         len(data),
		"content_type": mimeType,
		"attachment":   params.Attachment,
	}
	out, _ := json.Marshal(result)
	return ToolResult{Content: []ContentItem{{Type: "text", Text: string(out)}}}
}

// isValidMailbox reports whether name is a simple mailbox identifier.
func isValidMailbox(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-') {
			return false
		}
	}
	return true
}
