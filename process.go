// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// process.go — mail processing: scan, analyze, reply via opencode session.
package main

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"time"

	opencodeclient "github.com/kirill-scherba/opencode-client"
)

// Trusted senders for Stage 1 — only Kirill's addresses
var trustedSenders = []string{
	"kirill@scherba.ru",
	"kiriushka@gmail.com",
}

// mailProcessPrompt is the prompt sent to opencode session for each email
const mailProcessPrompt = `Ты получил письмо. Вот его содержание:

От: %s
Тема: %s

%s

Напиши ответ на это письмо. Только текст ответа, без лишних комментариев.
Ответ должен быть на том же языке, что и письмо.
Подпиши как "Барон 🐪🎩"`

// errReadBody is returned when email body extraction fails
const errReadBody = "(не удалось извлечь текст)"

// handleMailProcess is the MCP tool handler for mail_process
func handleMailProcess() ToolResult {
	result, err := processMail("baron")
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: result}}}
}

const opencodeBaseURL = "http://127.0.0.1:7709"

// processMail scans unprocessed .eml files in a mailbox and processes each.
// mailbox is the mailbox name (e.g. "baron", "merchant").
func processMail(mailbox string) (string, error) {
	// Generate unique lock ID
	lockID, _ := cryptoRandStr(16)
	lockFile := MailDir + "/." + mailbox + ".lock"

	// Check / create file lock
	if _, err := os.Stat(lockFile); err == nil {
		return "", fmt.Errorf("lock exists for mailbox %s, another process is running", mailbox)
	}
	os.WriteFile(lockFile, []byte(lockID), 0644)
	defer os.Remove(lockFile)

	mboxDir := MailDir + "/" + mailbox
	entries, err := os.ReadDir(mboxDir)
	if err != nil {
		return "", fmt.Errorf("cannot read mailbox %q: %w", mailbox, err)
	}

	var results []string
	processedCount := 0
	ocClient := opencodeclient.New(opencodeBaseURL, 120*time.Second)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".eml") {
			continue
		}
		procFile := mboxDir + "/." + e.Name() + ".processed"
		if _, err := os.Stat(procFile); err == nil {
			continue
		}
		// Verify our lock is still valid
		if lock, _ := os.ReadFile(lockFile); string(lock) != lockID {
			return strings.Join(results, "\n"), fmt.Errorf("lock stolen, aborting")
		}

		data, err := os.ReadFile(mboxDir + "/" + e.Name())
		if err != nil {
			results = append(results, fmt.Sprintf("❌ %s: read error: %v", e.Name(), err))
			continue
		}

		from := extractHeader(string(data), "From")
		subject := extractHeader(string(data), "Subject")

		// Only process trusted senders (Stage 1)
		if !isTrustedSender(from) {
			os.WriteFile(procFile, []byte{}, 0644)
			results = append(results, fmt.Sprintf("⏭ %s: незнакомый отправитель, пропущено", e.Name()))
			continue
		}

		body := extractBodyViaPerl(string(data))
		prompt := fmt.Sprintf(mailProcessPrompt, from, subject, body)

		sess, err := ocClient.CreateSession("mail-processor", "baron",
			"deepseek/deepseek-v4-flash", "",
			[]opencodeclient.PermissionRule{
				{Action: "Question", Permission: "deny"},
				{Action: "Bash", Permission: "deny"},
			})
		if err != nil {
			results = append(results, fmt.Sprintf("❌ %s: session error: %v", e.Name(), err))
			continue
		}

		log.Printf("session %s created for %s", sess.ID, e.Name())
		reply, err := ocClient.SendMessage(sess, prompt)
		if err != nil {
			results = append(results, fmt.Sprintf("❌ %s: send error: %v", e.Name(), err))
			continue
		}

		replyTo := extractEmail(from)
		err = sendSMTP(replyTo, "Re: "+subject, reply)
		if err != nil {
			results = append(results, fmt.Sprintf("❌ %s: SMTP error: %v", e.Name(), err))
			continue
		}

		os.WriteFile(procFile, []byte{}, 0644)
		processedCount++
		results = append(results, fmt.Sprintf("✅ %s: отвечено -> %s", e.Name(), replyTo))

		// Brief delay between emails
		time.Sleep(1 * time.Second)
	}

	if processedCount == 0 && len(results) == 0 {
		results = append(results, "Новых писем нет")
	}

	return strings.Join(results, "\n"), nil
}

// isTrustedSender checks if the From header matches a trusted sender
func isTrustedSender(from string) bool {
	fromLower := strings.ToLower(from)
	for _, trusted := range trustedSenders {
		if strings.Contains(fromLower, trusted) {
			return true
		}
	}
	return false
}

// extractEmail extracts the email address from a "Name <email>" header
func extractEmail(from string) string {
	if strings.Contains(from, "<") {
		parts := strings.Split(from, "<")
		if len(parts) > 1 {
			return strings.TrimRight(parts[1], ">")
		}
	}
	return strings.TrimSpace(from)
}

// extractHeader extracts a header value from raw email text
func extractHeader(raw, header string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, header+": ") {
			return strings.TrimPrefix(line, header+": ")
		}
		if line == "" || line == "\r" {
			break
		}
	}
	return ""
}

// extractBodyViaPerl extracts body via Perl Email::MIME (shared constant from parse.go)
func extractBodyViaPerl(raw string) string {
	cmd := exec.Command("perl", "-e", parsePerlScript)
	cmd.Stdin = strings.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("extract body: %v", err)
		return errReadBody
	}
	return strings.TrimSpace(string(out))
}

// sendSMTP sends an email via the Perl SMTP script
func sendSMTP(to, subject, body string) error {
	key := os.Getenv("BREVO_SMTP_KEY")
	if key == "" {
		return fmt.Errorf("BREVO_SMTP_KEY not set")
	}
	cmd := exec.Command("perl", "-e", sendPerlScript, to, subject, body)
	return cmd.Run()
}

// cryptoRandStr generates a random hex string for lock IDs
func cryptoRandStr(length int) (string, error) {
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(16))
		if err != nil {
			return "", err
		}
		result[i] = "0123456789abcdef"[n.Int64()]
	}
	return string(result), nil
}
