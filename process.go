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
	"sort"
	"strings"
	"time"

	opencodeclient "github.com/kirill-scherba/opencode-client"
)

// Trusted senders — only Kirill's addresses
var trustedSenders = []string{
	"kirill@scherba.ru",
	"kiriushka@gmail.com",
}

// mailProcessPrompt is sent to opencode session for each email
const mailProcessPrompt = `Ты получил письмо. Вот его содержание:

От: %s
Тема: %s

%s

Напиши ответ на это письмо. Только текст ответа, без лишних комментариев.
Ответ должен быть на том же языке, что и письмо.
Подпишись как положено — своим именем и своей подписью.`

const mailContextMaxLetters = 5

const errReadBody = "(не удалось извлечь текст)"

// handleMailProcess is the MCP tool handler for mail_process
func handleMailProcess(args *struct {
	Agent string `json:"agent"`
}) ToolResult {
	agent := "baron"
	if args != nil && args.Agent != "" {
		agent = args.Agent
	}
	result, err := processMail(agent)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: result}}}
}

const opencodeBaseURL = "http://127.0.0.1:7709"

// processMail scans unprocessed .eml files and processes each.
func processMail(agent string) (string, error) {
	lockID, _ := cryptoRandStr(16)
	lockFile := MailDir + "/." + agent + ".lock"

	if _, err := os.Stat(lockFile); err == nil {
		return "", fmt.Errorf("lock exists for agent %s, another process is running", agent)
	}
	os.WriteFile(lockFile, []byte(lockID), 0644)
	defer os.Remove(lockFile)

	mboxDir := MailDir + "/" + agent
	entries, err := os.ReadDir(mboxDir)
	if err != nil {
		return "", fmt.Errorf("cannot read mailbox %q: %w", agent, err)
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
		senderEmail := extractEmail(from)

		if !isTrustedSender(from) {
			os.WriteFile(procFile, []byte{}, 0644)
			results = append(results, fmt.Sprintf("⏭ %s: незнакомый отправитель (%s), пропущено", e.Name(), senderEmail))
			continue
		}

		body := extractBodyViaPerl(string(data))

		// Build context from recent emails with this sender
		context := buildSenderContext(mboxDir, senderEmail, e.Name())
		prompt := mailProcessPrompt
		if context != "" {
			prompt = "Вот последние письма с этим собеседником:\n" +
				context + "\n\n---\n\n" + mailProcessPrompt
		}
		prompt = fmt.Sprintf(prompt, from, subject, body)

		sess, err := ocClient.CreateSession("mail-processor", agent,
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

		if clsErr := ocClient.CloseSessionID(sess.ID); clsErr != nil {
			log.Printf("close session %s: %v", sess.ID, clsErr)
		}

		if err != nil {
			results = append(results, fmt.Sprintf("❌ %s: session send error: %v", e.Name(), err))
			continue
		}

		err = sendSMTP(senderEmail, "Re: "+subject, reply)
		if err != nil {
			results = append(results, fmt.Sprintf("❌ %s: SMTP error: %v", e.Name(), err))
			continue
		}

		// Save sent email copy
		saveSentEmail(mboxDir, senderEmail, "Re: "+subject, reply)

		os.WriteFile(procFile, []byte{}, 0644)
		processedCount++
		results = append(results, fmt.Sprintf("✅ %s: отвечено -> %s", e.Name(), senderEmail))
		time.Sleep(1 * time.Second)
	}

	if processedCount == 0 && len(results) == 0 {
		results = append(results, "Новых писем нет")
	}
	return strings.Join(results, "\n"), nil
}

// buildSenderContext reads last N emails from the same sender
func buildSenderContext(mboxDir, senderEmail, skipFile string) string {
	entries, err := os.ReadDir(mboxDir)
	if err != nil {
		return ""
	}
	type emailInfo struct {
		name string
		time int64
	}
	var emails []emailInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".eml") || e.Name() == skipFile {
			continue
		}
		data, _ := os.ReadFile(mboxDir + "/" + e.Name())
		from := extractHeader(string(data), "From")
		if !strings.Contains(from, senderEmail) {
			continue
		}
		info, _ := e.Info()
		if info != nil {
			emails = append(emails, emailInfo{name: e.Name(), time: info.ModTime().Unix()})
		}
	}
	sort.Slice(emails, func(i, j int) bool { return emails[i].time > emails[j].time })

	var parts []string
	for i, em := range emails {
		if i >= mailContextMaxLetters {
			break
		}
		data, _ := os.ReadFile(mboxDir + "/" + em.name)
		subj := extractHeader(string(data), "Subject")
		body := extractBodyViaPerl(string(data))
		parts = append(parts, fmt.Sprintf("--- Письмо: %s ---\n%s\n", subj, body))
	}
	return strings.Join(parts, "\n")
}

// saveSentEmail saves a copy of a sent email
func saveSentEmail(mboxDir, to, subject, body string) {
	sentDir := mboxDir + "/sent"
	os.MkdirAll(sentDir, 0755)
	now := time.Now().UTC()
	fname := fmt.Sprintf("%s_sent_%s.eml", now.Format("20060102_150405"),
		strings.SplitN(to, "@", 2)[0])
	msg := fmt.Sprintf("From: барон\nTo: %s\nSubject: %s\nDate: %s\n\n%s",
		to, subject, now.Format(time.RFC3339), body)
	os.WriteFile(sentDir+"/"+fname, []byte(msg), 0644)
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
			return strings.TrimRight(parts[1], ">\r")
		}
	}
	return strings.TrimSpace(from)
}

// extractHeader extracts a header value from raw email text
func extractHeader(raw, header string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, header+": ") {
			return strings.TrimRight(strings.TrimPrefix(line, header+": "), "\r")
		}
		if line == "" || line == "\r" {
			break
		}
	}
	return ""
}

// extractBodyViaPerl uses Perl Email::MIME to get text/plain body
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

// sendSMTP sends an email via Perl SMTP and saves sent copy
func sendSMTP(to, subject, body string) error {
	key := os.Getenv("BREVO_SMTP_KEY")
	if key == "" {
		return fmt.Errorf("BREVO_SMTP_KEY not set")
	}
	cmd := exec.Command("perl", "-e", sendPerlScript, to, subject, body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("SMTP: %s (exit %v)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// cryptoRandStr generates a random hex string
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
