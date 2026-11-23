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

	var served []string
	for _, box := range mailBoxes() {
		// Transit: the mailbox is not ours to answer, its letters wait for the installation that owns it.
		if box.Handler == "" {
			log.Printf("poll %s: транзитный ящик, обработчик не назначен", box.Address)
			continue
		}
		served = append(served, box.Address+":"+box.Handler)
		go func(mailbox, handler string) {
			time.Sleep(10 * time.Second)
			for {
				result, err := processMailbox(mailbox, handler)
				if err != nil {
					log.Printf("poll %s: %v", mailbox, err)
				} else if result != "Новых писем нет" {
					log.Printf("poll %s: %s", mailbox, result)
				}
				time.Sleep(interval)
			}
		}(box.Address, box.Handler)
	}

	log.Printf("mail poller started (interval=%s, boxes=%s)", interval, strings.Join(served, ", "))
}
