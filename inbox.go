// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// inbox.go — HTTP inbox server, receives email from Cloudflare Email Routing.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"time"
)

const inboxPort = ":8901"

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
	extractAttachments(mboxDir, fname, body)

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

// extractAttachments saves attachments from a raw email into an attachments subdir.
func extractAttachments(mboxDir, emlName string, raw []byte) {
	// Write raw email to a temp file so Perl can parse it
	tmpFile := mboxDir + "/." + emlName + ".tmp"
	if err := os.WriteFile(tmpFile, raw, 0644); err != nil {
		log.Printf("attachments tmp write %s: %v", emlName, err)
		return
	}
	defer os.Remove(tmpFile)

	// Call Perl to extract attachments — it returns count
	// We create attDir only if there are actual attachments.
	tmpOut := mboxDir + "/." + emlName + ".att"
	cmd := exec.Command("perl", "-e", extractAttachPerlScript, tmpFile, tmpOut)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("attachments extract %s: %s (err: %v)", emlName, string(out), err)
		os.RemoveAll(tmpOut)
		return
	}

	// If Perl created files in tmpOut, move them to final dir
	if entries, _ := os.ReadDir(tmpOut); len(entries) > 0 {
		attDir := mboxDir + "/attachments/" + emlName
		os.MkdirAll(attDir, 0755)
		if err := os.Rename(tmpOut, attDir); err != nil {
			log.Printf("attachments move %s: %v", emlName, err)
		}
	} else {
		os.RemoveAll(tmpOut)
	}

	log.Printf("attachments for %s: %s", emlName, string(out))
}
