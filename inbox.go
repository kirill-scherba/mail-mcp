// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// inbox.go — HTTP inbox server, receives email from Cloudflare Email Routing.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/quotedprintable"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Inbox listens on loopback: Cloudflare and nginx deliver to it from this machine.
var inboxPort = func() string {
	if v := os.Getenv("MAIL_MCP_INBOX_ADDR"); v != "" {
		return v
	}
	return "127.0.0.1:8901"
}()

// startInboxServer starts the HTTP server that receives emails from Cloudflare.
func startInboxServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/mail/inbox", handleInboxPost)

	server := &http.Server{
		Addr:         inboxPort,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("Inbox server starting on %s", inboxPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("inbox server: %v", err)
		}
	}()
}

// handleInboxPost receives an email POST from Cloudflare Worker,
// determines the target mailbox from the To header, saves the raw .eml,
// and extracts attachments.
func handleInboxPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify inbox secret if configured
	if secret := os.Getenv("INBOX_SECRET"); secret != "" {
		if r.Header.Get("X-Inbox-Secret") != secret {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Decode quoted-printable (Cloudflare encodes multipart as QP)
	if decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(string(body)))); err == nil && len(decoded) > 0 {
		body = decoded
	}

	// Determine mailbox from To: header (local-part@matrica.work)
	mailbox := extractMailboxFromRaw(body)
	mboxDir := MailDir + "/" + mailbox
	os.MkdirAll(mboxDir, 0755)

	// Generate unique filename
	now := time.Now().UTC()
	fname := fmt.Sprintf("%s_%d.eml", now.Format("20060102_150405"), os.Getpid())

	// Save raw email
	if err := os.WriteFile(mboxDir+"/"+fname, body, 0644); err != nil {
		log.Printf("inbox save %s: %v", fname, err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	// Extract attachments
	ensureAttachments(mboxDir, fname, body)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// extractMailboxFromRaw parses the To header from raw email bytes.
func extractMailboxFromRaw(raw []byte) string {
	re := regexp.MustCompile(`(?m)^To:\s*(.*)$`)
	match := re.FindSubmatch(raw)
	if len(match) < 2 {
		return defaultMailbox
	}
	to := string(match[1])
	re2 := regexp.MustCompile(`([\w.-]+)@matrica\.work`)
	m2 := re2.FindStringSubmatch(to)
	if len(m2) < 2 {
		return defaultMailbox
	}
	return m2[1]
}

// ensureAttachments returns attachment file names for the given email.
// If the attachment directory already exists, returns its contents.
// Otherwise, extracts attachments from the raw email via Perl.
func ensureAttachments(mboxDir, emlName string, raw []byte) []string {
	attDir := mboxDir + "/attachments/" + emlName
	// Already extracted?
	if entries, err := os.ReadDir(attDir); err == nil && len(entries) > 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		return names
	}

	// Decode quoted-printable (Cloudflare encodes multipart as QP)
	decoded, decErr := io.ReadAll(quotedprintable.NewReader(strings.NewReader(string(raw))))
	if decErr == nil && len(decoded) > 0 {
		raw = decoded
	}

	// Extract via Perl
	tmpFile := mboxDir + "/." + emlName + ".tmp"
	if err := os.WriteFile(tmpFile, raw, 0644); err != nil {
		log.Printf("attachments tmp write %s: %v", emlName, err)
		return nil
	}
	defer os.Remove(tmpFile)

	tmpOut := mboxDir + "/." + emlName + ".att"
	os.MkdirAll(tmpOut, 0755)
	cmd := exec.Command("perl", "-e", extractAttachPerlScript, tmpFile, tmpOut)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("attachments extract %s: %s (err: %v)", emlName, string(out), err)
		os.RemoveAll(tmpOut)
		return nil
	}

	entries, err := os.ReadDir(tmpOut)
	if err != nil || len(entries) == 0 {
		os.RemoveAll(tmpOut)
		return nil
	}

	os.RemoveAll(attDir)
	if err := os.Rename(tmpOut, attDir); err != nil {
		log.Printf("attachments move %s: %v", emlName, err)
		os.RemoveAll(tmpOut)
		return nil
	}

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	log.Printf("attachments for %s: %v", emlName, names)
	return names
}
