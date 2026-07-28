// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// gmail.go — Gmail OAuth2 integration: auth, list, read, send via Gmail API.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	gmailTokenFile    = "/srv/mail/baron/gmail_token.json"
	gmailCallbackPort = ":9999"
	gmailAPIBase      = "https://gmail.googleapis.com/gmail/v1/users/me"
)

var gmailOAuthConfig *oauth2.Config

func initGmailOAuth() {
	clientID := os.Getenv("GMAIL_CLIENT_ID")
	clientSecret := os.Getenv("GMAIL_CLIENT_SECRET")
	redirectURI := os.Getenv("GMAIL_REDIRECT_URI")
	if clientID == "" || clientSecret == "" || redirectURI == "" {
		return // Gmail not configured, tools will return error
	}
	gmailOAuthConfig = &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Scopes:       []string{"https://www.googleapis.com/auth/gmail.readonly", "https://www.googleapis.com/auth/gmail.send"},
		Endpoint:     google.Endpoint,
	}
}

// gmailGetToken returns stored token or nil.
func gmailGetToken() (*oauth2.Token, error) {
	data, err := os.ReadFile(gmailTokenFile)
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// gmailSaveToken stores the token to file.
func gmailSaveToken(tok *oauth2.Token) error {
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(gmailTokenFile, data, 0644)
}

// gmailHTTPClient returns an authenticated HTTP client using stored token.
// If token is expired, it refreshes automatically.
func gmailHTTPClient() (*http.Client, error) {
	tok, err := gmailGetToken()
	if err != nil {
		return nil, fmt.Errorf("no token: %w — run gmail_auth first", err)
	}
	client := gmailOAuthConfig.Client(context.Background(), tok)
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

// gmailCallbackHandler receives the OAuth code from Google and exchanges it for a token.
func gmailCallbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	tok, err := gmailOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("token exchange error: %v", err)
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	if err := gmailSaveToken(tok); err != nil {
		log.Printf("save token error: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	w.Write([]byte(`<!DOCTYPE html><html><body><h1>✅ Gmail авторизован!</h1><p>Можно закрыть окно.</p></body></html>`))
	log.Printf("Gmail token saved successfully (expires: %v)", tok.Expiry)
}

// gmailAuthURL generates the OAuth consent URL for the user.
func gmailAuthURL() string {
	if gmailOAuthConfig == nil {
		return "GMAIL_CLIENT_ID / GMAIL_CLIENT_SECRET / GMAIL_REDIRECT_URI not set in env"
	}
	authURL := gmailOAuthConfig.AuthCodeURL("state", oauth2.AccessTypeOffline)
	return fmt.Sprintf("Перейдите по ссылке, разрешите доступ:\n\n%s\n\nПосле этого вы будете перенаправлены на %s", authURL, gmailOAuthConfig.RedirectURL)
}

// gmailDoAPI performs an authenticated GET request to the Gmail API.
func gmailDoAPI(endpoint string) ([]byte, error) {
	client, err := gmailHTTPClient()
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
func handleGmailAuth() ToolResult {
	url := gmailAuthURL()
	return ToolResult{Content: []ContentItem{{Type: "text", Text: url}}}
}

// handleGmailList is the MCP tool handler for gmail_list
func handleGmailList() ToolResult {
	data, err := gmailDoAPI("/messages?maxResults=10")
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: string(data)}}}
}

// handleGmailSend sends an email via Gmail API
func handleGmailSend(to, subject, body string) ToolResult {
	client, err := gmailHTTPClient()
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}

	subjectEnc := mime.BEncoding.Encode("utf-8", subject)
	raw := fmt.Sprintf("From: me\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", to, subjectEnc, body)
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
