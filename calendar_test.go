// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestEventsURLExpandsRecurring: a calendar screen must see the Monday meeting on every Monday, so
// events.list is asked with singleEvents=true, and orderBy=startTime is legal only together with it.
func TestEventsURLExpandsRecurring(t *testing.T) {
	min := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	max := min.AddDate(0, 1, 0)
	raw := calendarEventsURL("primary", "Europe/Moscow", min, max, 250, "")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if got := q.Get("singleEvents"); got != "true" {
		t.Errorf("singleEvents = %q, want true", got)
	}
	if got := q.Get("orderBy"); got != "startTime" {
		t.Errorf("orderBy = %q, want startTime", got)
	}
	if got := q.Get("timeZone"); got != "Europe/Moscow" {
		t.Errorf("timeZone = %q", got)
	}
	if got := q.Get("maxResults"); got != "250" {
		t.Errorf("maxResults = %q, want 250", got)
	}
	if got := q.Get("pageToken"); got != "" {
		t.Errorf("pageToken = %q, want empty on the first page", got)
	}
}

// TestEventsURLPageToken: the second page carries the token and nothing else changes.
func TestEventsURLPageToken(t *testing.T) {
	min := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	raw := calendarEventsURL("calendar@example.com", "UTC", min, min.Add(time.Hour), 100, "tok-2")
	u, _ := url.Parse(raw)
	if got := u.Query().Get("pageToken"); got != "tok-2" {
		t.Errorf("pageToken = %q", got)
	}
	if !strings.Contains(u.Path, "calendars/calendar@example.com/events") {
		t.Errorf("path = %q", u.Path)
	}
}

// TestCalendarListURLPages: Google caps one calendarList page at 250 entries.
func TestCalendarListURLPages(t *testing.T) {
	u, err := url.Parse(calendarListURL("tok"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := u.Query().Get("maxResults"); got != "250" {
		t.Errorf("maxResults = %q, want 250", got)
	}
	if got := u.Query().Get("pageToken"); got != "tok" {
		t.Errorf("pageToken = %q", got)
	}
	if !strings.HasSuffix(u.Path, "/users/me/calendarList") {
		t.Errorf("path = %q", u.Path)
	}
}

// TestEventJSONOfTimed: a timed event keeps its offset and is not all-day.
func TestEventJSONOfTimed(t *testing.T) {
	var e calendarEvent
	e.ID = "abc"
	e.Summary = "Совещание"
	e.Location = "Кабинет 3"
	e.Start.DateTime = "2026-11-21T10:00:00+03:00"
	e.Start.TimeZone = "Europe/Moscow"
	e.End.DateTime = "2026-11-21T11:00:00+03:00"
	e.Attendees = []calendarAttendee{{Email: "a@example.com", ResponseStatus: "accepted"}}

	out := eventJSONOf(e, "primary")
	if out.AllDay {
		t.Error("all_day = true for a timed event")
	}
	if out.Start != e.Start.DateTime || out.End != e.End.DateTime {
		t.Errorf("start/end = %q/%q", out.Start, out.End)
	}
	if out.TimeZone != "Europe/Moscow" {
		t.Errorf("time_zone = %q", out.TimeZone)
	}
	if len(out.Attendees) != 1 || out.Attendees[0].Email != "a@example.com" {
		t.Errorf("attendees = %+v", out.Attendees)
	}

	// The JSON the screen reads uses snake_case keys and a fixed shape.
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"all_day":false`, `"calendar_id":"primary"`, `"start":"2026-11-21T10:00:00+03:00"`, `"response_status":"accepted"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("json %s does not contain %s", raw, key)
		}
	}
}

// TestEventJSONOfAllDay: an all-day event is a date pair, and its end is the day after the last day.
func TestEventJSONOfAllDay(t *testing.T) {
	var e calendarEvent
	e.ID = "day"
	e.Summary = "Отпуск"
	e.Start.Date = "2026-11-21"
	e.End.Date = "2026-11-22"

	out := eventJSONOf(e, "primary")
	if !out.AllDay {
		t.Fatal("all_day = false for a date-only event")
	}
	if out.Start != "2026-11-21" || out.End != "2026-11-22" {
		t.Errorf("start/end = %q/%q", out.Start, out.End)
	}
}

// TestEventJSONOfRecurringInstance: an expanded occurrence names its master, so a screen can edit one
// occurrence or the whole series.
func TestEventJSONOfRecurringInstance(t *testing.T) {
	var e calendarEvent
	e.ID = "instance_20261123"
	e.RecurringEventID = "master"
	e.Start.DateTime = "2026-11-23T10:00:00+03:00"
	e.End.DateTime = "2026-11-23T11:00:00+03:00"

	out := eventJSONOf(e, "primary")
	if out.RecurringEventID != "master" {
		t.Errorf("recurring_event_id = %q", out.RecurringEventID)
	}
}

// TestEventSpanForCreate: a date pair makes one whole day when both dates are the same; a timestamp
// pair keeps its times; a mixture is refused.
func TestEventSpanForCreate(t *testing.T) {
	start, end, allDay, err := eventSpanForCreate("2026-11-21", "2026-11-21")
	if err != nil {
		t.Fatalf("same-day pair: %v", err)
	}
	if !allDay {
		t.Error("all_day = false for dates")
	}
	if end.Sub(start) != 24*time.Hour {
		t.Errorf("span = %v, want 24h", end.Sub(start))
	}

	start, end, allDay, err = eventSpanForCreate("2026-11-21", "2026-11-24")
	if err != nil || !allDay || end.Sub(start) != 72*time.Hour {
		t.Errorf("four-day span: all_day=%v span=%v err=%v", allDay, end.Sub(start), err)
	}

	start, end, allDay, err = eventSpanForCreate("2026-11-21T10:00:00+03:00", "2026-11-21T11:30:00+03:00")
	if err != nil || allDay || end.Sub(start) != 90*time.Minute {
		t.Errorf("timed span: all_day=%v span=%v err=%v", allDay, end.Sub(start), err)
	}

	if _, _, _, err = eventSpanForCreate("2026-11-21", "2026-11-21T11:00:00+03:00"); err == nil {
		t.Error("a date with a timestamp must be refused")
	}
	if _, _, _, err = eventSpanForCreate("2026-11-21T11:00:00+03:00", "2026-11-21T10:00:00+03:00"); err == nil {
		t.Error("an end before the start must be refused")
	}
}

// TestParseEventTimeRejectsGarbage: the error names both accepted shapes.
func TestParseEventTimeRejectsGarbage(t *testing.T) {
	_, _, err := parseEventTime("завтра", "start")
	if err == nil {
		t.Fatal("accepted a word instead of a time")
	}
	if !strings.Contains(err.Error(), "2026-11-21T10:00:00+03:00") || !strings.Contains(err.Error(), "2026-11-21") {
		t.Errorf("error does not show the accepted shapes: %v", err)
	}
}

// TestFormatCalendarList: the text answer names the primary calendar and keeps the id readable.
func TestFormatCalendarList(t *testing.T) {
	text := formatCalendarList([]calendarEntry{
		{ID: "kirill@example.com", Summary: "Кирилл", Primary: true, AccessRole: "owner"},
		{ID: "ru.russian#holiday@group.v.calendar.google.com", Summary: "Праздники", AccessRole: "reader"},
	})
	if !strings.Contains(text, "(основной)") {
		t.Errorf("no primary mark: %s", text)
	}
	if !strings.Contains(text, "ru.russian#holiday@group.v.calendar.google.com") {
		t.Errorf("id missing: %s", text)
	}
	if !strings.Contains(text, "Календари: 2") {
		t.Errorf("count missing: %s", text)
	}
}

// TestCalendarsJSON: the structured list carries the colour a screen paints the calendar with.
func TestCalendarsJSON(t *testing.T) {
	out := calendarEntriesJSON([]calendarEntry{
		{ID: "c1", Summary: "Работа", BackgroundColor: "#7986cb", AccessRole: "writer", Selected: true},
	})
	if len(out) != 1 {
		t.Fatalf("len = %d", len(out))
	}
	raw, _ := json.Marshal(out[0])
	for _, key := range []string{`"background_color":"#7986cb"`, `"access_role":"writer"`, `"selected":true`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("json %s does not contain %s", raw, key)
		}
	}
}
