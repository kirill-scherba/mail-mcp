package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
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
			Description: "Send an email via Brevo SMTP.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"to":      {"type": "string", "description": "Recipient email"},
					"subject": {"type": "string", "description": "Email subject"},
					"body":    {"type": "string", "description": "Email body text"}
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
			Name:        "waiting_list",
			Description: "List pending (waiting) emails — those waiting for replies from others.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"agent": {"type": "string", "default": "baron"}
				}
			}`),
		},
		{
			Name:        "waiting_create",
			Description: "Create a waiting item for an email that needs info from someone else. Saves the pending request and sends questions to specified recipients.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file": {"type": "string", "description": "Original email filename"},
					"from": {"type": "string", "description": "Original sender email"},
					"subject": {"type": "string", "description": "Original email subject"},
					"needs": {"type": "array", "description": "List of people to ask", "items": {
						"type": "object",
						"properties": {
							"who": {"type": "string", "description": "Email of person to ask"},
							"question": {"type": "string", "description": "Question to ask them"}
						}
					}},
					"agent": {"type": "string", "default": "baron"}
				}
			}`),
		},
		{
			Name:        "gmail_auth",
			Description: "Start Gmail OAuth2 authorization. Returns a URL to visit for granting access.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "gmail_list",
			Description: "List recent messages from Gmail (requires gmail_auth first).",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "gmail_send",
			Description: "Send an email via Gmail API.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"to":      {"type": "string", "description": "Recipient email"},
					"subject": {"type": "string", "description": "Subject"},
					"body":    {"type": "string", "description": "Email body"}
				},
				"required": ["to", "subject", "body"]
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
	case "waiting_list":
		return handleWaitingList(call.Arguments)
	case "waiting_create":
		return handleWaitingCreate(call.Arguments)
	case "gmail_auth":
		return handleGmailAuth()
	case "gmail_list":
		return handleGmailList()
	case "gmail_send":
		var p struct {
			To      string `json:"to"`
			Subject string `json:"subject"`
			Body    string `json:"body"`
		}
		json.Unmarshal(call.Arguments, &p)
		return handleGmailSend(p.To, p.Subject, p.Body)
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

	return ToolResult{Content: []ContentItem{{Type: "text", Text: string(out)}}}
}

func handleMailSend(args json.RawMessage) ToolResult {
	var params struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	json.Unmarshal(args, &params)

	if params.To == "" || params.Subject == "" || params.Body == "" {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Missing required fields"}}}
	}

	// Call Perl to send via Brevo SMTP relay
	cmd := exec.Command("perl", "-e", sendPerlScript,
		defaultFromName, defaultFromAddr, params.To, params.Subject, params.Body)

	out, err := cmd.Output()
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Send error: " + string(out) + err.Error()}}}
	}

	return ToolResult{Content: []ContentItem{{Type: "text", Text: "✅ " + string(out)}}}
}
