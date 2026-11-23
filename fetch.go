// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// fetch.go — passive mail: an installation without its own Cloudflare route takes letters from another
// mail-mcp that has one.
//
//	server side:  POST /mail/fetch {"address":"pro_tools@matrica.work"} → letters as base64 + ids
//	              POST /mail/ack   {"ids":["…"]}                        → those letters are delivered
//	client side:  every MAIL_SOURCE_INTERVAL, fetch, write .eml into its own mailbox folder, ack
//
// The letters land in the same folder a Cloudflare delivery would use, so the rest of the program - the
// poller, the attachments, the agent - does not know the difference (23.11.2026).
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fetchMessage is one letter handed over.
type fetchMessage struct {
	ID      string `json:"id"`      // file name in the mailbox folder
	Address string `json:"address"` // mailbox it belongs to
	Raw     string `json:"raw"`     // base64 of the .eml
}

// writeJSON answers with a JSON body: the intake speaks JSON only.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fetchToken answers the addresses one token may take, from MAIL_FETCH_TOKENS:
//
//	tokA:pro_tools@matrica.work,tender@matrica.work;tokB:baron@matrica.work
//
// A token is the whole right: no token - no letters, and it may only name the addresses written for it.
var fetchTokens = func() map[string][]string {
	out := map[string][]string{}
	for _, pair := range strings.Split(os.Getenv("MAIL_FETCH_TOKENS"), ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			continue
		}
		token := strings.TrimSpace(parts[0])
		addresses := []string{}
		for _, a := range strings.Split(parts[1], ",") {
			if s := strings.ToLower(strings.TrimSpace(a)); s != "" {
				addresses = append(addresses, s)
			}
		}
		if token != "" && len(addresses) > 0 {
			out[token] = addresses
		}
	}
	return out
}()

// mailFetchHandler gives the letters of one mailbox to the installation that owns it.
func mailFetchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
	allowed := fetchTokens[token]
	if len(allowed) == 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var in struct {
		Address string `json:"address"`
		Limit   int    `json:"limit"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in)
	address := strings.ToLower(strings.TrimSpace(in.Address))
	permitted := false
	for _, a := range allowed {
		if a == address {
			permitted = true
			break
		}
	}
	if !permitted {
		http.Error(w, "forbidden: this token does not serve that mailbox", http.StatusForbidden)
		return
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	dir := filepath.Join(MailDir, address)
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, 200, map[string]any{"messages": []fetchMessage{}})
		return
	}
	messages := []fetchMessage{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".eml") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		messages = append(messages, fetchMessage{ID: e.Name(), Address: address,
			Raw: base64.StdEncoding.EncodeToString(body)})
		if len(messages) >= limit {
			break
		}
	}
	writeJSON(w, 200, map[string]any{"messages": messages})
}

// mailAckHandler confirms delivery: a letter leaves the source only when the other side has it on disk.
func mailAckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
	allowed := fetchTokens[token]
	if len(allowed) == 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var in struct {
		Address string   `json:"address"`
		IDs     []string `json:"ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	address := strings.ToLower(strings.TrimSpace(in.Address))
	permitted := false
	for _, a := range allowed {
		if a == address {
			permitted = true
			break
		}
	}
	if !permitted {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	dir := filepath.Join(MailDir, address)
	delivered := filepath.Join(dir, "_delivered")
	_ = os.MkdirAll(delivered, 0o755)
	acked := 0
	for _, id := range in.IDs {
		name := filepath.Base(strings.TrimSpace(id))
		if name == "" || name == "." || name == string(filepath.Separator) {
			continue
		}
		if err := os.Rename(filepath.Join(dir, name), filepath.Join(delivered, name)); err == nil {
			acked++
		}
	}
	writeJSON(w, 200, map[string]any{"acked": acked})
}

// startMailFetch pulls mail from a source installation: the passive half of the same scheme.
func startMailFetch() {
	source := strings.TrimRight(strings.TrimSpace(os.Getenv("MAIL_SOURCE_URL")), "/")
	token := strings.TrimSpace(os.Getenv("MAIL_SOURCE_TOKEN"))
	address := strings.ToLower(strings.TrimSpace(os.Getenv("MAIL_SOURCE_ADDRESS")))
	if source == "" || token == "" || address == "" {
		return
	}
	interval := time.Minute
	if s := os.Getenv("MAIL_SOURCE_INTERVAL"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d >= 5*time.Second {
			interval = d
		}
	}
	log.Printf("mail fetch: %s → %s every %s", address, source, interval)
	go func() {
		for {
			if n, err := fetchOnce(source, token, address); err != nil {
				log.Printf("mail fetch: %v", err)
			} else if n > 0 {
				log.Printf("mail fetch: принято писем: %d", n)
			}
			time.Sleep(interval)
		}
	}()
}

// runIntake is the receiver as a service: the port, the mailboxes and - when asked - the processing of the
// letters that belong to this installation. No MCP, no Google callback: an internet entrance has no business
// carrying either (23.11.2026).
func runIntake() {
	if strings.TrimSpace(os.Getenv("MAIL_MCP_INBOX_ADDR")) == "" {
		log.Fatal("mail intake: MAIL_MCP_INBOX_ADDR is required")
	}
	startInboxServer()
	if strings.EqualFold(os.Getenv("MAIL_MCP_POLL"), "on") {
		startMailPoller()
	}
	select {}
}

// runMailFetch is the passive installation's own loop: it takes letters and does nothing else, so it can
// be started by a scheduler, a container entrypoint or by hand - it does not need an MCP client.
func runMailFetch() {
	source := strings.TrimRight(strings.TrimSpace(os.Getenv("MAIL_SOURCE_URL")), "/")
	token := strings.TrimSpace(os.Getenv("MAIL_SOURCE_TOKEN"))
	addresses := []string{}
	for _, a := range strings.Split(os.Getenv("MAIL_SOURCE_ADDRESS"), ",") {
		if s := strings.ToLower(strings.TrimSpace(a)); s != "" {
			addresses = append(addresses, s)
		}
	}
	if source == "" || token == "" || len(addresses) == 0 {
		log.Fatal("mail fetch: MAIL_SOURCE_URL, MAIL_SOURCE_TOKEN and MAIL_SOURCE_ADDRESS are required")
	}
	interval := time.Minute
	if s := os.Getenv("MAIL_SOURCE_INTERVAL"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d >= 5*time.Second {
			interval = d
		}
	}
	log.Printf("mail fetch: %s ← %s every %s", strings.Join(addresses, ", "), source, interval)
	for {
		for _, address := range addresses {
			if n, err := fetchOnce(source, token, address); err != nil {
				log.Printf("mail fetch %s: %v", address, err)
			} else if n > 0 {
				log.Printf("mail fetch %s: принято писем: %d", address, n)
			}
		}
		time.Sleep(interval)
	}
}

// fetchOnce takes one batch and confirms it; letters are written before the confirmation.
func fetchOnce(source, token, address string) (int, error) {
	body, _ := json.Marshal(map[string]any{"address": address, "limit": 20})
	req, err := http.NewRequest(http.MethodPost, source+"/mail/fetch", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("источник ответил %s", resp.Status)
	}
	var out struct {
		Messages []fetchMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	if len(out.Messages) == 0 {
		return 0, nil
	}
	dir := filepath.Join(MailDir, address)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	ids := []string{}
	for _, m := range out.Messages {
		raw, err := base64.StdEncoding.DecodeString(m.Raw)
		if err != nil {
			continue
		}
		name := filepath.Base(m.ID)
		if name == "" || name == "." {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			continue
		}
		ensureAttachments(dir, name, raw)
		ids = append(ids, name)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	// The acknowledgement goes last: a letter that was not written stays on the source.
	ackBody, _ := json.Marshal(map[string]any{"address": address, "ids": ids})
	ack, err := http.NewRequest(http.MethodPost, source+"/mail/ack", bytes.NewReader(ackBody))
	if err != nil {
		return len(ids), err
	}
	ack.Header.Set("Content-Type", "application/json")
	ack.Header.Set("Authorization", "Bearer "+token)
	if resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(ack); err == nil {
		resp.Body.Close()
	} else {
		return len(ids), fmt.Errorf("письма приняты, подтверждение не ушло: %w", err)
	}
	return len(ids), nil
}
