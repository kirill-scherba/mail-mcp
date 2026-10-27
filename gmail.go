// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// gmail.go — Gmail OAuth2 integration and shared Google OAuth helpers.
// Calendar reuses the same OAuth config, token store, and refresh logic.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Loopback by default: the callback reaches the internet through nginx, never by ip:port.
// MAIL_MCP_CALLBACK_ADDR exists for the odd case of an OAuth client that insists on a public address.
var gmailCallbackPort = func() string {
	if v := os.Getenv("MAIL_MCP_CALLBACK_ADDR"); v != "" {
		return v
	}
	return "127.0.0.1:9999"
}()

const (
	gmailAPIBase      = "https://gmail.googleapis.com/gmail/v1/users/me"
)

// googleTokenPath returns the token file path for an account.
// Empty account falls back to the legacy single-token path.
func googleTokenPath(account string) string {
	if account == "" {
		return "/srv/mail/baron/gmail_token.json"
	}
	return "/srv/mail/gmail/" + account + "/token.json"
}

// googleDefaultAccount returns the default account for an operation
// (read or send), from env or empty.
func googleDefaultAccount(use string) string {
	if use == "send" {
		return os.Getenv("GMAIL_DEFAULT_SEND_ACCOUNT")
	}
	return os.Getenv("GMAIL_DEFAULT_READ_ACCOUNT")
}

var googleOAuthConfig *oauth2.Config

func initGmailOAuth() {
	clientID := os.Getenv("GMAIL_CLIENT_ID")
	clientSecret := os.Getenv("GMAIL_CLIENT_SECRET")
	redirectURI := os.Getenv("GMAIL_REDIRECT_URI")
	if clientID == "" || clientSecret == "" || redirectURI == "" {
		return // Gmail not configured, tools will return error
	}
	googleOAuthConfig = &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Scopes: []string{
			"https://www.googleapis.com/auth/gmail.readonly",
			"https://www.googleapis.com/auth/gmail.send",
			"https://www.googleapis.com/auth/calendar",
		},
		Endpoint: google.Endpoint,
	}
}

// googleGetToken returns stored token for an account or nil.
func googleGetToken(account string) (*oauth2.Token, error) {
	data, err := os.ReadFile(googleTokenPath(account))
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// googleSaveToken stores the token for an account to file.
func googleSaveToken(account string, tok *oauth2.Token) error {
	path := googleTokenPath(account)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// googleHTTPClient returns an authenticated HTTP client using stored token
// for the account. If token is expired, it refreshes automatically.
func googleHTTPClient(account string) (*http.Client, error) {
	tok, err := googleGetToken(account)
	if err != nil {
		return nil, fmt.Errorf("no token: %w — run gmail_auth first", err)
	}
	client := googleOAuthConfig.Client(context.Background(), tok)
	return client, nil
}

// startGmailCallbackServer starts a minimal HTTP server for OAuth callback.
func startGmailCallbackServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", gmailCallbackHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{
		Addr:         gmailCallbackPort,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("Gmail callback server starting on %s", gmailCallbackPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("gmail callback server: %v", err)
		}
	}()
}

// gmailCallbackHandler receives the OAuth code from Google and exchanges it
// for a token. The OAuth state carries the account (email) to save under.
func gmailCallbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	account := r.URL.Query().Get("state")
	if account == "" || account == "state" {
		account = googleDefaultAccount("read")
	}

	tok, err := googleOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("token exchange error: %v", err)
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	if err := googleSaveToken(account, tok); err != nil {
		log.Printf("save token error: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	if tok.RefreshToken == "" {
		log.Printf("WARNING: no refresh_token issued for %s — token will expire and auth will break again", account)
	}

	w.Write([]byte(`<!DOCTYPE html><html><body><h1>✅ Gmail авторизован!</h1><p>Можно закрыть окно.</p></body></html>`))
	log.Printf("Gmail token saved for %s (expires: %v)", account, tok.Expiry)
}

// gmailAuthURL generates the OAuth consent URL for the user. The state
// carries the account so the callback can save the token under its key.
func gmailAuthURL(account string) string {
	if googleOAuthConfig == nil {
		return "GMAIL_CLIENT_ID / GMAIL_CLIENT_SECRET / GMAIL_REDIRECT_URI not set in env"
	}
	if account == "" {
		account = googleDefaultAccount("read")
	}
	// prompt=consent forces Google to show the consent screen again and
	// issue a NEW refresh token on every authorization. Without it, a
	// repeated auth for the same client_id only returns a short-lived
	// access token, which silently breaks the mail reader after ~1 hour.
	authURL := googleOAuthConfig.AuthCodeURL(account,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	return fmt.Sprintf("Перейдите по ссылке, разрешите доступ для %s:\n\n%s\n\nПосле этого вы будете перенаправлены на %s", account, authURL, googleOAuthConfig.RedirectURL)
}

// gmailDoAPI performs an authenticated GET request to the Gmail API.
func gmailDoAPI(account, endpoint string) ([]byte, error) {
	client, err := googleHTTPClient(account)
	if err != nil {
		return nil, err
	}
	url := gmailAPIBase + endpoint
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: %s", resp.Status)
	}
	buf := make([]byte, 0)
	for {
		chunk := make([]byte, 32768)
		n, err := resp.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}

// handleGmailAuth is the MCP tool handler for gmail_auth
func handleGmailAuth(account string) ToolResult {
	url := gmailAuthURL(account)
	return ToolResult{Content: []ContentItem{{Type: "text", Text: url}}}
}

// gmailMeta is a compact message summary (headers only, no body).
type gmailMeta struct {
	ID      string
	From    string
	Subject string
	Date    string
	Snippet string
}

// gmailFetchMeta fetches message metadata (From/Subject/Date + snippet)
// without downloading the full body. Falls back to the snippet on errors
// so a single broken message never breaks a whole list/search.
func gmailFetchMeta(account, messageID string) (gmailMeta, error) {
	client, err := googleHTTPClient(account)
	if err != nil {
		return gmailMeta{}, err
	}
	u := gmailAPIBase + "/messages/" + url.PathEscape(messageID) +
		"?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date"
	resp, err := client.Get(u)
	if err != nil {
		return gmailMeta{}, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return gmailMeta{}, fmt.Errorf("api error: %s", resp.Status)
	}
	if len(buf) == 0 {
		return gmailMeta{}, fmt.Errorf("empty response")
	}

	var msg struct {
		ID      string `json:"id"`
		Snippet string `json:"snippet"`
		Payload struct {
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(buf, &msg); err != nil {
		return gmailMeta{}, fmt.Errorf("parse: %w", err)
	}
	headers := map[string]string{}
	for _, h := range msg.Payload.Headers {
		headers[strings.ToLower(h.Name)] = h.Value
	}
	return gmailMeta{
		ID:      msg.ID,
		From:    headers["from"],
		Subject: headers["subject"],
		Date:    headers["date"],
		Snippet: strings.TrimSpace(msg.Snippet),
	}, nil
}

// gmailListMeta returns message IDs from /messages (optionally with a query).
func gmailListMeta(account, query string, limit int) ([]gmailMeta, int, error) {
	client, err := googleHTTPClient(account)
	if err != nil {
		return nil, 0, err
	}
	u := gmailAPIBase + "/messages?maxResults=" + fmt.Sprintf("%d", limit)
	if query != "" {
		u += "&q=" + url.QueryEscape(query)
	}
	resp, err := client.Get(u)
	if err != nil {
		return nil, 0, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("api error: %s", resp.Status)
	}
	var res struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		ResultSizeEstimate int `json:"resultSizeEstimate"`
	}
	if err := json.Unmarshal(buf, &res); err != nil {
		return nil, 0, fmt.Errorf("parse: %w", err)
	}
	metas := make([]gmailMeta, 0, len(res.Messages))
	for _, m := range res.Messages {
		meta, err := gmailFetchMeta(account, m.ID)
		if err != nil {
			// Keep the ID so the message is still listed, even if headers fail.
			metas = append(metas, gmailMeta{ID: m.ID, Subject: "(headers unavailable)"})
			continue
		}
		metas = append(metas, meta)
	}
	return metas, res.ResultSizeEstimate, nil
}

// handleGmailList is the MCP tool handler for gmail_list
func handleGmailList(limit int, account string) ToolResult {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	if account == "" {
		account = googleDefaultAccount("read")
	}
	metas, total, err := gmailListMeta(account, "", limit)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Ящик: %s | Писем в почте (оценка): %d, показано %d:\n\n", account, total, len(metas)))
	for i, m := range metas {
		sb.WriteString(fmt.Sprintf("[%d] %s\n    From: %s\n    Date: %s\n    ID: %s\n\n", i+1, m.Subject, m.From, m.Date, m.ID))
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: sb.String()}}}
}

// handleGmailSearch searches Gmail messages by query and returns metadata.
func handleGmailSearch(query string, limit int, account string) ToolResult {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	if account == "" {
		account = googleDefaultAccount("read")
	}
	metas, total, err := gmailListMeta(account, query, limit)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Ящик: %s | Найдено писем (оценка): %d, показано %d:\n\n", account, total, len(metas)))
	for i, m := range metas {
		sb.WriteString(fmt.Sprintf("[%d] %s\n    From: %s\n    Date: %s\n    ID: %s\n    %s\n\n",
			i+1, m.Subject, m.From, m.Date, m.ID, truncateStr(m.Snippet, 200)))
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: sb.String()}}}
}

// truncateStr cuts a string to n runes.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// handleGmailRead reads a full Gmail message by ID
func handleGmailRead(messageID, account string) ToolResult {
	client, err := googleHTTPClient(account)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}

	url := gmailAPIBase + "/messages/" + messageID + "?format=full"
	resp, err := client.Get(url)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("api error: %v", err)}}}
	}
	defer resp.Body.Close()

	buf, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("api error: %s", resp.Status)}}}
	}
	if len(buf) == 0 {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "empty response from Gmail API"}}}
	}

	// Parse and format
	var msg struct {
		ID      string `json:"id"`
		Thread  string `json:"threadId"`
		Snippet string `json:"snippet"`
		Payload struct {
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
			MimeType string `json:"mimeType"`
			Body     struct {
				Data string `json:"data"`
			} `json:"body"`
			Parts []struct {
				MimeType string `json:"mimeType"`
				Filename string `json:"filename"`
				Body     struct {
					Data string `json:"data"`
				} `json:"body"`
			} `json:"parts"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(buf, &msg); err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("parse error: %v", err)}}}
	}

	headers := map[string]string{}
	for _, h := range msg.Payload.Headers {
		headers[strings.ToLower(h.Name)] = h.Value
	}

	var sb strings.Builder
	sb.WriteString("From: " + headers["from"] + "\n")
	sb.WriteString("To: " + headers["to"] + "\n")
	sb.WriteString("Subject: " + headers["subject"] + "\n")
	sb.WriteString("Date: " + headers["date"] + "\n")
	sb.WriteString("---\n")

	// Find text/plain body (first part or root)
	bodyText := ""
	if msg.Payload.Body.Data != "" {
		bodyText = decodeBase64Str(msg.Payload.Body.Data)
	} else {
		for _, p := range msg.Payload.Parts {
			if p.MimeType == "text/plain" && p.Body.Data != "" {
				bodyText = decodeBase64Str(p.Body.Data)
				break
			}
		}
	}
	if bodyText == "" {
		bodyText = msg.Snippet
	}
	// Strip HTML if body is HTML
	if strings.Contains(bodyText, "<") && strings.Contains(bodyText, ">") {
		bodyText = stripHTML(bodyText)
	}
	if bodyText == "" {
		bodyText = msg.Snippet
	}
	sb.WriteString(bodyText)

	return ToolResult{Content: []ContentItem{{Type: "text", Text: sb.String()}}}
}

// stripHTML removes HTML tags, style/script blocks, and decodes common entities
func stripHTML(s string) string {
	// Remove style and script blocks entirely.
	// NOTE: RE2 (Go regexp) has no backreferences, so the tag name is
	// repeated instead of using \1.
	blockRe := regexp.MustCompile(`(?is)<(style|script)\b[^>]*>.*?</(style|script)>`)
	s = blockRe.ReplaceAllString(s, " ")
	// Remove comments
	s = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(s, " ")
	// Remove remaining tags
	re := regexp.MustCompile(`<[^>]+>`)
	s = re.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	// Collapse multiple spaces
	space := regexp.MustCompile(`\s+`)
	s = space.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// decodeBase64Str decodes base64 (with URL-safe variant support)
func decodeBase64Str(s string) string {
	if decoded, err := base64.URLEncoding.DecodeString(s); err == nil {
		return string(decoded)
	}
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
		return string(decoded)
	}
	return s
}

// handleGmailSend sends an email via Gmail API, optionally with attachment.
func handleGmailSend(to, subject, body, attachmentPath, account string) ToolResult {
	if account == "" {
		account = googleDefaultAccount("send")
	}
	client, err := googleHTTPClient(account)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}

	subjectEnc := mime.BEncoding.Encode("utf-8", subject)
	var raw string

	if attachmentPath == "" {
		// Plain text
		raw = fmt.Sprintf("From: me\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", to, subjectEnc, body)
	} else {
		// Multipart with attachment
		attData, err := os.ReadFile(attachmentPath)
		if err != nil {
			return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("attachment read error: %v", err)}}}
		}
		boundary := "boundary_123_matrica"
		_, attName := filepath.Split(attachmentPath)
		attB64 := base64.StdEncoding.EncodeToString(attData)
		raw = fmt.Sprintf("From: me\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%s\r\n\r\n--%s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n--%s\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"%s\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n--%s--",
			to, subjectEnc, boundary,
			boundary, body,
			boundary, attName, attB64,
			boundary)
	}

	encoded := base64.URLEncoding.EncodeToString([]byte(raw))
	payload := fmt.Sprintf(`{"raw":"%s"}`, encoded)
	resp, err := client.Post(gmailAPIBase+"/messages/send", "application/json", strings.NewReader(payload))
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("send error: %v", err)}}}
	}
	defer resp.Body.Close()

	buf := make([]byte, 0)
	chunk := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err != nil {
			break
		}
	}

	return ToolResult{Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("✅ Sent: %s", string(buf))}}}
}
