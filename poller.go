// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// poller.go — periodic mail polling inside mail-mcp.
package main

import (
	"log"
	"os"
	"strings"
	"time"
)

const (
	defaultPollInterval = 5 * time.Second
	defaultPollAgents   = "baron,merchant"
)

// startMailPoller runs periodic mail processing for all configured agents.
func startMailPoller() {
	interval := defaultPollInterval
	if s := os.Getenv("MAIL_POLL_INTERVAL"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			interval = d
		}
	}

	agents := defaultPollAgents
	if s := os.Getenv("MAIL_AGENTS"); s != "" {
		agents = s
	}
	agentList := strings.Split(agents, ",")

	for _, agent := range agentList {
		agent := strings.TrimSpace(agent)
		if agent == "" {
			continue
		}
		go func(a string) {
			time.Sleep(10 * time.Second)
			for {
				result, err := processMail(a)
				if err != nil {
					log.Printf("poll %s: %v", a, err)
				} else if result != "Новых писем нет" {
					log.Printf("poll %s: %s", a, result)
				}
				time.Sleep(interval)
			}
		}(agent)
	}

	log.Printf("mail poller started (interval=%s, agents=%s)", interval, agents)
}
