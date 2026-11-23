// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// boxes.go — which agent handles which mailbox.
//
// A mailbox is an address. The agent that answers its letters is a separate name, and a mailbox may have
// none: that is a transit mailbox - the letters wait there for the installation they belong to, and no
// agent runs, not even when someone asks by hand (23.11.2026).
//
//	MAIL_BOXES="baron@matrica.work:baron;pro_tools@matrica.work:"
//	                                        ^ no handler - transit
package main

import (
	"os"
	"strings"
)

// mailBox is one mailbox and the agent assigned to it.
type mailBox struct {
	Address string // folder name: the whole address, or an agent name of the old style
	Handler string // agent that processes its letters; empty means transit
}

// mailBoxes answers the mailboxes of this installation. MAIL_BOXES is the setting; MAIL_AGENTS (a plain
// list of agent names, each also naming its mailbox) and the two default agents keep old installations
// working unchanged.
func mailBoxes() []mailBox {
	out := []mailBox{}
	if raw := strings.TrimSpace(os.Getenv("MAIL_BOXES")); raw != "" {
		for _, item := range strings.Split(raw, ";") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			parts := strings.SplitN(item, ":", 2)
			address := strings.ToLower(strings.TrimSpace(parts[0]))
			handler := ""
			if len(parts) == 2 {
				handler = strings.ToLower(strings.TrimSpace(parts[1]))
			}
			if address == "" {
				continue
			}
			out = append(out, mailBox{Address: address, Handler: handler})
		}
		return out
	}
	agents := defaultPollAgents
	if s := strings.TrimSpace(os.Getenv("MAIL_AGENTS")); s != "" {
		agents = s
	}
	for _, a := range strings.Split(agents, ",") {
		if a = strings.ToLower(strings.TrimSpace(a)); a != "" {
			out = append(out, mailBox{Address: a, Handler: a})
		}
	}
	return out
}

// isTransitMailbox says whether the mailbox has no handler at all.
func isTransitMailbox(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	local := name
	if i := strings.Index(name, "@"); i > 0 {
		local = name[:i]
	}
	for _, b := range mailBoxes() {
		if b.Handler != "" {
			continue
		}
		// The same mailbox may be named by its address or by its local part: both must be refused.
		entryLocal := b.Address
		if i := strings.Index(entryLocal, "@"); i > 0 {
			entryLocal = entryLocal[:i]
		}
		if b.Address == name || entryLocal == local {
			return true
		}
	}
	return false
}
