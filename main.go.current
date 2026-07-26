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

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 || os.Args[1] == "serve" {
		serveMCP()
		return
	}
	fmt.Println("mail-mcp MCP server. Usage: mail-mcp serve")
}

func serveMCP() {
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
					"page": {"type": "integer", "description": "Page number (1-based)", "default": 1},
					"limit": {"type": "integer", "description": "Items per page", "default": 10},
					"unread_only": {"type": "boolean", "description": "Show only unread", "default": false}
				}
			}`),
		},
		{
			Name:        "mail_read",
			Description: "Read a full email by filename. Decodes headers and body. Marks as read.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"mailbox": {"type": "string", "description": "Mailbox name", "default": "baron"},
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
					"to": {"type": "string", "description": "Recipient email"},
					"subject": {"type": "string", "description": "Email subject"},
					"body": {"type": "string", "description": "Email body text"}
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
	default:
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Unknown tool: " + call.Name}}}
	}
}

func handleMailList(args json.RawMessage) ToolResult {
	var params struct {
		Mailbox    string `json:"mailbox"`
		Page       int    `json:"page"`
		Limit      int    `json:"limit"`
		UnreadOnly bool   `json:"unread_only"`
	}
	json.Unmarshal(args, &params)
	if params.Mailbox == "" { params.Mailbox = "baron" }
	if params.Page < 1 { params.Page = 1 }
	if params.Limit < 1 { params.Limit = 10 }

	mailboxDir := MailDir + "/" + params.Mailbox
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
		File    string `json:"file"`
	}
	json.Unmarshal(args, &params)
	if params.Mailbox == "" { params.Mailbox = "baron" }

	emailPath := MailDir + "/" + params.Mailbox + "/" + params.File
	if _, err := os.Stat(emailPath); os.IsNotExist(err) {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Email not found"}}}
	}

	// Call Perl parser
	cmd := exec.Command("perl", "-e", `
		use Email::MIME;
		use Encode;
		my $raw = do { local $/; <STDIN> };
		my $email = Email::MIME->new($raw);
		print "From: " . $email->header('From') . "\n";
		print "To: " . $email->header('To') . "\n";
		print "Subject: " . decode('MIME-Header', $email->header('Subject')) . "\n";
		print "Date: " . $email->header('Date') . "\n";
		print "---\n";
		for my $part ($email->parts) {
			my $ct = $part->content_type || '';
			if ($ct =~ m|text/plain|i) {
				print $part->body_str;
				last;
			}
		}
		if (!scalar($email->parts)) {
			print $email->body_str;
		}
	`)
	data, _ := os.ReadFile(emailPath)
	cmd.Stdin = strings.NewReader(string(data))
	out, err := cmd.Output()
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Parse error: " + err.Error()}}}
	}

	// Mark as read
	readFile := MailDir + "/" + params.Mailbox + "/." + params.File + ".read"
	os.WriteFile(readFile, []byte{}, 0644)

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

	// Call Python to send via Brevo API
	cmd := exec.Command("python3", "-c", `
import urllib.request, json, sys
data = json.dumps({
    "sender": {"name": "Барон", "email": "baron@matrica.work"},
    "to": [{"email": sys.argv[1]}],
    "subject": sys.argv[2],
    "htmlContent": "<pre>" + sys.argv[3] + "</pre>"
}).encode()
req = urllib.request.Request(
    "https://api.brevo.com/v3/smtp/email",
    data=data,
    headers={
        "api-key": "REDACTED-BREVO-KEY",
        "Content-Type": "application/json"
    },
    method="POST"
)
resp = urllib.request.urlopen(req, timeout=30)
print("Sent:", json.loads(resp.read()).get("messageId", "ok"))
`, params.To, params.Subject, params.Body)

	out, err := cmd.Output()
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Send error: " + string(out) + err.Error()}}}
	}

	return ToolResult{Content: []ContentItem{{Type: "text", Text: "✅ " + string(out)}}}
}
