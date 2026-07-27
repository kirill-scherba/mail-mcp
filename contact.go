// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.
// contact.go — contact book stored as JSON files in /srv/contacts/
package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const contactsDir = "/srv/contacts"

// Contact represents a mail correspondent
type Contact struct {
	Email     string `json:"email"`
	Name      string `json:"name,omitempty"`
	Who       string `json:"who,omitempty"`
	Summary   string `json:"summary,omitempty"`
	FirstSeen string `json:"first_seen,omitempty"`
	LastSeen  string `json:"last_seen,omitempty"`
	TotalMail int    `json:"total_mail"`
	IsSpam    bool   `json:"is_spam,omitempty"`
}

func contactKey(email string) string {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email)))))
	return contactsDir + "/" + hash + ".json"
}

func handleContactGet(args json.RawMessage) ToolResult {
	var p struct{ Email string `json:"email"` }
	json.Unmarshal(args, &p)
	if p.Email == "" {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Missing email"}}}
	}
	data, err := os.ReadFile(contactKey(p.Email))
	if err != nil {
		return ToolResult{Content: []ContentItem{{Type: "text", Text: "Contact not found"}}}
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: string(data)}}}
}

func handleContactSave(args json.RawMessage) ToolResult {
	var p struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Who     string `json:"who"`
		Summary string `json:"summary"`
	}
	json.Unmarshal(args, &p)
	if p.Email == "" {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Missing email"}}}
	}

	os.MkdirAll(contactsDir, 0755)
	key := contactKey(p.Email)

	// Load existing if any
	var c Contact
	if data, err := os.ReadFile(key); err == nil {
		json.Unmarshal(data, &c)
	}

	c.Email = p.Email
	if p.Name != "" { c.Name = p.Name }
	if p.Who != "" { c.Who = p.Who }
	if p.Summary != "" { c.Summary = p.Summary }
	if c.FirstSeen == "" { c.FirstSeen = time.Now().UTC().Format("2006-01-02") }
	c.LastSeen = time.Now().UTC().Format("2006-01-02")
	c.TotalMail++

	data, _ := json.MarshalIndent(c, "", "  ")
	os.WriteFile(key, data, 0644)
	return ToolResult{Content: []ContentItem{{Type: "text", Text: "✅ Contact saved: " + p.Email}}}
}

func ensureContact(from string) *Contact {
	email := extractEmail(from)
	if email == "" { return nil }
	email = strings.ToLower(email)

	os.MkdirAll(contactsDir, 0755)
	key := contactKey(email)

	var c Contact
	if data, err := os.ReadFile(key); err == nil {
		json.Unmarshal(data, &c)
	} else {
		c.Email = email
		c.Name = extractName(from)
		c.FirstSeen = time.Now().UTC().Format("2006-01-02")
	}

	c.LastSeen = time.Now().UTC().Format("2006-01-02")
	c.TotalMail++
	data, _ := json.MarshalIndent(c, "", "  ")
	os.WriteFile(key, data, 0644)
	return &c
}

func extractName(from string) string {
	if idx := strings.Index(from, "<"); idx > 0 {
		return strings.TrimSpace(from[:idx])
	}
	if idx := strings.Index(from, "@"); idx > 0 {
		return from[:idx]
	}
	return from
}
