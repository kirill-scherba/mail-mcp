// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// waiting.go — отложенные письма (ожидают ответа от кого-то).
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WaitingNeed describes one question asked to someone
type WaitingNeed struct {
	Who         string `json:"who"`
	Question    string `json:"question"`
	Status      string `json:"status"` // pending / asked / answered
	AskedAt     string `json:"asked_at,omitempty"`
	AnswerEmail string `json:"answer_email,omitempty"`
}

// WaitingItem describes a pending email waiting for answers
type WaitingItem struct {
	ID           string        `json:"id"`
	Agent        string        `json:"agent"`
	OriginalFile string        `json:"original_file"`
	From         string        `json:"from"`
	Subject      string        `json:"subject"`
	ReplySent    bool          `json:"reply_sent"` // вежливый ответ уже отправлен?
	Needs        []WaitingNeed `json:"needs"`
	CreatedAt    string        `json:"created_at"`
	TimeoutAt    string        `json:"timeout_at,omitempty"`
}

func waitingDir(agent string) string {
	return filepath.Join(MailDir, agent, "waiting")
}

func waitingPath(agent, id string) string {
	return filepath.Join(waitingDir(agent), id+".json")
}

func genID() string {
	const chars = "0123456789abcdef"
	res := make([]byte, 16)
	for i := range res {
		n, _ := rand.Int(rand.Reader, big.NewInt(16))
		res[i] = chars[n.Int64()]
	}
	return string(res)
}

// waitingSave writes a waiting item to disk
func waitingSave(w *WaitingItem) error {
	dir := waitingDir(w.Agent)
	os.MkdirAll(dir, 0755)
	data, _ := json.MarshalIndent(w, "", "  ")
	return os.WriteFile(waitingPath(w.Agent, w.ID), data, 0644)
}

// waitingLoad loads all waiting items for an agent
func waitingLoad(agent string) []*WaitingItem {
	dir := waitingDir(agent)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var items []*WaitingItem
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var w WaitingItem
		if json.Unmarshal(data, &w) == nil {
			items = append(items, &w)
		}
	}
	return items
}

// waitingDelete removes a waiting item
func waitingDelete(agent, id string) error {
	return os.Remove(waitingPath(agent, id))
}

// waitingCreate creates a new waiting item and returns it
func waitingCreate(agent, from, subject, originalFile string) *WaitingItem {
	now := time.Now().UTC()
	w := &WaitingItem{
		ID:           genID(),
		Agent:        agent,
		OriginalFile: originalFile,
		From:         from,
		Subject:      subject,
		CreatedAt:    now.Format(time.RFC3339),
		TimeoutAt:    now.Add(24 * time.Hour).Format(time.RFC3339),
	}
	waitingSave(w)
	return w
}

// waitingCheck returns a summary of waiting items for the agent prompt
// and handles timeout actions (reminders, closure).
// Returns: "У вас N отложенных писем. ..."
func waitingCheck(agent string) string {
	items := waitingLoad(agent)
	if len(items) == 0 {
		return ""
	}

	now := time.Now().UTC()
	var open, overdue, needReminder int

	for _, w := range items {
		// Count needs by status
		pendingNeeds := 0
		for _, n := range w.Needs {
			switch n.Status {
			case "pending":
				pendingNeeds++
			case "asked":
				// Check if reminder needed (5h)
				if n.AskedAt != "" {
					asked, err := time.Parse(time.RFC3339, n.AskedAt)
					if err == nil && now.Sub(asked) > 5*time.Hour {
						needReminder++
					}
				}
			case "answered":
				// All good
			}
		}

		open++
		_ = pendingNeeds

		// Check timeout (24h)
		if w.TimeoutAt != "" {
			to, err := time.Parse(time.RFC3339, w.TimeoutAt)
			if err == nil && now.After(to) {
				overdue++
			}
		}
	}

	parts := []string{}
	parts = append(parts, fmt.Sprintf("У вас %d отложенных писем.", open))
	if overdue > 0 {
		parts = append(parts, fmt.Sprintf("%d просрочено (более 24ч).", overdue))
	}
	if needReminder > 0 {
		parts = append(parts, fmt.Sprintf("%d вопросов требуют напоминания (>5ч).", needReminder))
	}
	return strings.Join(parts, " ")
}

// waitingMatch checks if an email matches any waiting need
// Returns the waiting item and the matched need index, or nil
func waitingMatch(agent, from, subject string) (*WaitingItem, int) {
	items := waitingLoad(agent)
	if len(items) == 0 {
		return nil, -1
	}
	subject = strings.ToLower(subject)
	for _, w := range items {
		for i, n := range w.Needs {
			if n.Status == "answered" {
				continue
			}
			// Check if this email is from someone we asked
			fromLower := strings.ToLower(from)
			if !strings.Contains(fromLower, strings.ToLower(n.Who)) {
				continue
			}
			// Check subject: should be a reply to our question
			// Our question was about the original subject
			if strings.Contains(subject, strings.ToLower(w.Subject[:20])) {
				return w, i
			}
		}
	}
	return nil, -1
}

// ─── MCP tool handlers ───────────────────────────────────────────────────

func handleWaitingList(args json.RawMessage) ToolResult {
	var p struct {
		Agent string `json:"agent"`
	}
	json.Unmarshal(args, &p)
	if p.Agent == "" {
		p.Agent = "baron"
	}
	items := waitingLoad(p.Agent)
	if len(items) == 0 {
		return ToolResult{Content: []ContentItem{{Type: "text", Text: "Нет отложенных писем"}}}
	}
	data, _ := json.MarshalIndent(items, "", "  ")
	return ToolResult{Content: []ContentItem{{Type: "text", Text: string(data)}}}
}
