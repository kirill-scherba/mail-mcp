// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// session_store.go — persistent session reuse across emails from the same sender.
package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	opencodeclient "github.com/kirill-scherba/opencode-client"
)

const (
	defaultSessionMax   = 30
	sessionStoreFile    = "sessions.json"
)

// SessionEntry holds one sender's session metadata.
type SessionEntry struct {
	Email     string `json:"email"`
	SessionID string `json:"session_id"`
	Count     int    `json:"count"`
	Max       int    `json:"max"`
	UpdatedAt int64  `json:"updated_at"`
}

// SessionStore manages reusable opencode sessions per sender email.
type SessionStore struct {
	mu       sync.Mutex
	filePath string
	entries  map[string]*SessionEntry
}

// NewSessionStore creates a store backed by a JSON file in mboxDir.
func NewSessionStore(mboxDir string) *SessionStore {
	s := &SessionStore{
		filePath: mboxDir + "/" + sessionStoreFile,
		entries:  make(map[string]*SessionEntry),
	}
	s.load()
	return s
}

// Get returns an existing session for the email if it exists, is still valid,
// and has not exceeded Max messages. Returns nil otherwise.
func (s *SessionStore) Get(email string) *SessionEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[email]
	if !ok {
		return nil
	}
	if e.Count >= e.Max {
		return nil
	}
	return e
}

// Set creates or updates a session entry for the given email.
func (s *SessionStore) Set(email, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[email]; ok {
		e.SessionID = sessionID
		e.Count = 0
		e.UpdatedAt = time.Now().Unix()
	} else {
		s.entries[email] = &SessionEntry{
			Email:     email,
			SessionID: sessionID,
			Count:     0,
			Max:       defaultSessionMax,
			UpdatedAt: time.Now().Unix(),
		}
	}
}

// Inc increments the message counter for the given email.
func (s *SessionStore) Inc(email string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[email]; ok {
		e.Count++
		e.UpdatedAt = time.Now().Unix()
	}
}

// Delete removes the session entry for the given email.
func (s *SessionStore) Delete(email string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, email)
}

// load reads sessions from the JSON file.
func (s *SessionStore) load() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("session store load: %v", err)
		}
		return
	}
	var entries []*SessionEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Printf("session store decode: %v", err)
		return
	}
	for _, e := range entries {
		s.entries[e.Email] = e
	}
}

// Save persists sessions to the JSON file.
func (s *SessionStore) Save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := make([]*SessionEntry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		log.Printf("session store encode: %v", err)
		return
	}
	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		log.Printf("session store save: %v", err)
	}
}

// getOrCreateSession tries to reuse a cached session (if within limit and alive).
// If the stored session fails on SendMessage, it is discarded and a new one is created.
func getOrCreateSession(ocClient *opencodeclient.Client, store *SessionStore, email, agent string) (*opencodeclient.Session, error) {
	// Try cached session — no ping, just return it.
	// If it's dead the caller will get an error and can retry with a new one.
	if entry := store.Get(email); entry != nil {
		return &opencodeclient.Session{ID: entry.SessionID, Agent: agent}, nil
	}

	// Create new session
	sess, err := ocClient.CreateSession("mail-processor", agent,
		"deepseek/deepseek-v4-flash", "",
		[]opencodeclient.PermissionRule{
			{Action: "Question", Permission: "deny"},
			{Action: "Bash", Permission: "deny"},
		})
	if err != nil {
		return nil, err
	}

	store.Set(email, sess.ID)
	return sess, nil
}
