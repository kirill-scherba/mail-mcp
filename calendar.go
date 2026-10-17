// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by a BSD-style license.

// calendar.go — Google Calendar API integration for mail-mcp.
// Talks to the Calendar API over raw HTTP using the same OAuth2 config,
// token store and refresh logic as Gmail.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	calendarAPIBase           = "https://www.googleapis.com/calendar/v3"
	calendarDefaultCalendarID = "primary"
	calendarMaxResults        = 250
)

// calendarDefaultTimeZone returns the configured default time zone,
// falling back to Europe/Moscow.
func calendarDefaultTimeZone() string {
	if tz := os.Getenv("CALENDAR_DEFAULT_TIMEZONE"); tz != "" {
		return tz
	}
	return "Europe/Moscow"
}

// calendarEvent is the subset of the Google Calendar Event resource we use.
type calendarEvent struct {
	ID          string   `json:"id,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Location    string   `json:"location,omitempty"`
	Status      string   `json:"status,omitempty"`
	HTMLLink    string   `json:"htmlLink,omitempty"`
	Recurrence  []string `json:"recurrence,omitempty"`
	Start       struct {
		DateTime string `json:"dateTime,omitempty"`
		Date     string `json:"date,omitempty"`
		TimeZone string `json:"timeZone,omitempty"`
	} `json:"start,omitempty"`
	End struct {
		DateTime string `json:"dateTime,omitempty"`
		Date     string `json:"date,omitempty"`
		TimeZone string `json:"timeZone,omitempty"`
	} `json:"end,omitempty"`
	Attendees []struct {
		Email          string `json:"email,omitempty"`
		DisplayName    string `json:"displayName,omitempty"`
		ResponseStatus string `json:"responseStatus,omitempty"`
	} `json:"attendees,omitempty"`
}

// calendarEventsResponse is the envelope returned by events.list.
type calendarEventsResponse struct {
	Items []calendarEvent `json:"items"`
}

// calendarDo issues an authenticated Calendar API request and returns the
// response body plus status. It maps Google auth/scope errors to actionable
// messages matching the existing Gmail style.
func calendarDo(account, method, rawURL string, body []byte) ([]byte, int, error) {
	client, err := googleHTTPClient(account)
	if err != nil {
		return nil, 0, err
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("bad request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "invalid_grant") || strings.Contains(errStr, "invalid_credentials") {
			return nil, 0, fmt.Errorf("token expired/invalid — run gmail_auth to re-authorize")
		}
		return nil, 0, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return data, resp.StatusCode, nil
	}

	var googleErr struct {
		Error struct {
			Code    int    `json:"code"`
			Status  string `json:"status"`
			Message string `json:"message"`
			Errors  []struct {
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &googleErr)

	reasons := make([]string, 0, len(googleErr.Error.Errors))
	for _, e := range googleErr.Error.Errors {
		reasons = append(reasons, e.Reason)
	}
	reasonStr := strings.Join(reasons, ", ")
	if reasonStr == "" {
		reasonStr = googleErr.Error.Status
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, resp.StatusCode, fmt.Errorf("token expired/invalid (status: %s) — run gmail_auth to re-authorize", reasonStr)
	case resp.StatusCode == http.StatusForbidden && (strings.Contains(reasonStr, "insufficientPermissions") || strings.Contains(reasonStr, "insufficientScopes")):
		return nil, resp.StatusCode, fmt.Errorf("token lacks the calendar scope — re-run gmail_auth")
	default:
		msg := googleErr.Error.Message
		if msg == "" {
			msg = string(data)
		}
		return nil, resp.StatusCode, fmt.Errorf("calendar api error %d (%s): %s", resp.StatusCode, reasonStr, msg)
	}
}

// calendarEventsURL builds the events.list URL with the mandatory parameters.
func calendarEventsURL(calendarID, tz string, timeMin, timeMax time.Time, maxResults int) string {
	u, _ := url.Parse(calendarAPIBase + "/calendars/" + url.PathEscape(calendarID) + "/events")
	q := u.Query()
	q.Set("timeMin", timeMin.Format(time.RFC3339))
	q.Set("timeMax", timeMax.Format(time.RFC3339))
	q.Set("maxResults", fmt.Sprintf("%d", maxResults))
	q.Set("timeZone", tz)
	u.RawQuery = q.Encode()
	return u.String()
}

// parseRFC3339 parses a Calendar timestamp argument and returns an explicit
// error if the value is missing the mandatory time zone offset.
func parseRFC3339(value, field string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("%s is required", field)
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC 3339 with time zone offset, e.g. 2026-10-20T00:00:00+03:00", field)
	}
	return t, nil
}

// calendarListEvents lists events in the requested range, leaving recurring
// masters intact (singleEvents is not set). Results are sorted client-side.
func calendarListEvents(account, calendarID, tz string, timeMin, timeMax time.Time, maxResults int) ([]calendarEvent, error) {
	if calendarID == "" {
		calendarID = calendarDefaultCalendarID
	}
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > calendarMaxResults {
		maxResults = calendarMaxResults
	}

	rawURL := calendarEventsURL(calendarID, tz, timeMin, timeMax, maxResults)
	body, _, err := calendarDo(account, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	var resp calendarEventsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse events: %w", err)
	}

	sort.Slice(resp.Items, func(i, j int) bool {
		return eventLess(resp.Items[i], resp.Items[j], tz)
	})

	return resp.Items, nil
}

// eventStartTime extracts a comparable start time from an event.
// The second return value is true for all-day events (date-only).
func eventStartTime(e calendarEvent, defaultTZ string) (time.Time, bool, bool) {
	if e.Start.Date != "" {
		t, err := time.Parse("2006-01-02", e.Start.Date)
		if err == nil {
			return t, true, true
		}
	}
	if e.Start.DateTime != "" {
		t, err := time.Parse(time.RFC3339, e.Start.DateTime)
		if err == nil {
			return t, false, true
		}
	}
	return time.Time{}, false, false
}

// eventLess reports whether event a should sort before event b.
// All-day events on the same calendar day sort before timed events.
func eventLess(a, b calendarEvent, defaultTZ string) bool {
	at, aAllDay, aOK := eventStartTime(a, defaultTZ)
	bt, bAllDay, bOK := eventStartTime(b, defaultTZ)
	if !aOK || !bOK {
		return aOK && !bOK
	}
	if !at.Equal(bt) {
		return at.Before(bt)
	}
	if aAllDay != bAllDay {
		return aAllDay // all-day before timed
	}
	return a.ID < b.ID
}

// calendarGetEvent fetches a single event by ID.
func calendarGetEvent(account, calendarID, eventID string) (calendarEvent, error) {
	if calendarID == "" {
		calendarID = calendarDefaultCalendarID
	}
	rawURL := calendarAPIBase + "/calendars/" + url.PathEscape(calendarID) + "/events/" + url.PathEscape(eventID)
	body, _, err := calendarDo(account, http.MethodGet, rawURL, nil)
	if err != nil {
		return calendarEvent{}, err
	}
	var e calendarEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return calendarEvent{}, fmt.Errorf("parse event: %w", err)
	}
	return e, nil
}

// attendeeEmails converts a slice of email strings into Calendar attendee objects.
func attendeeEmails(emails []string) []map[string]string {
	out := make([]map[string]string, 0, len(emails))
	for _, email := range emails {
		if email = strings.TrimSpace(email); email != "" {
			out = append(out, map[string]string{"email": email})
		}
	}
	return out
}

// calendarCreateEvent creates a new event. Attendees are not notified
// (sendUpdates=none) by design.
func calendarCreateEvent(account, calendarID string, e calendarEvent) (calendarEvent, error) {
	if calendarID == "" {
		calendarID = calendarDefaultCalendarID
	}

	rawURL := calendarAPIBase + "/calendars/" + url.PathEscape(calendarID) + "/events?sendUpdates=none"
	body, _, err := calendarDo(account, http.MethodPost, rawURL, mustEncode(e))
	if err != nil {
		return calendarEvent{}, err
	}
	var created calendarEvent
	if err := json.Unmarshal(body, &created); err != nil {
		return calendarEvent{}, fmt.Errorf("parse created event: %w", err)
	}
	return created, nil
}

// calendarPatchEvent applies a partial update. Attendees are not re-notified
// (sendUpdates=none) by design.
func calendarPatchEvent(account, calendarID, eventID string, patch map[string]any) (calendarEvent, error) {
	if calendarID == "" {
		calendarID = calendarDefaultCalendarID
	}

	rawURL := calendarAPIBase + "/calendars/" + url.PathEscape(calendarID) + "/events/" + url.PathEscape(eventID) + "?sendUpdates=none"
	body, _, err := calendarDo(account, http.MethodPatch, rawURL, mustEncode(patch))
	if err != nil {
		return calendarEvent{}, err
	}
	var updated calendarEvent
	if err := json.Unmarshal(body, &updated); err != nil {
		return calendarEvent{}, fmt.Errorf("parse updated event: %w", err)
	}
	return updated, nil
}

// calendarDeleteEvent removes an event. Attendees are not notified.
func calendarDeleteEvent(account, calendarID, eventID string) error {
	if calendarID == "" {
		calendarID = calendarDefaultCalendarID
	}
	rawURL := calendarAPIBase + "/calendars/" + url.PathEscape(calendarID) + "/events/" + url.PathEscape(eventID) + "?sendUpdates=none"
	_, status, err := calendarDo(account, http.MethodDelete, rawURL, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d on delete", status)
	}
	return nil
}

// mustEncode JSON-encodes v, panicking only on programmer error.
func mustEncode(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// formatEventTime renders an event's start/end as a compact string.
// All-day events show only the date; timed events show local date/time in each
// event's own offset.
func formatEventTime(e calendarEvent) string {
	if e.Start.Date != "" && e.End.Date != "" {
		return fmt.Sprintf("%s (весь день)", e.Start.Date)
	}
	start, _ := time.Parse(time.RFC3339, e.Start.DateTime)
	end, _ := time.Parse(time.RFC3339, e.End.DateTime)
	if start.IsZero() || end.IsZero() {
		return "время неизвестно"
	}
	if start.Format("2006-01-02") == end.Format("2006-01-02") {
		return fmt.Sprintf("%s %s–%s", start.Format("2006-01-02"), start.Format("15:04"), end.Format("15:04"))
	}
	return fmt.Sprintf("%s %s – %s %s", start.Format("2006-01-02"), start.Format("15:04"), end.Format("2006-01-02"), end.Format("15:04"))
}

// formatEvent renders one event as compact human-readable text.
func formatEvent(e calendarEvent, idx int) string {
	var sb strings.Builder
	if idx > 0 {
		sb.WriteString(fmt.Sprintf("[%d] ", idx))
	}
	sb.WriteString(fmt.Sprintf("%s — %s\n", formatEventTime(e), e.Summary))
	if e.Location != "" {
		sb.WriteString(fmt.Sprintf("    Место: %s\n", e.Location))
	}
	if len(e.Attendees) > 0 {
		names := make([]string, 0, len(e.Attendees))
		for _, a := range e.Attendees {
			name := a.DisplayName
			if name == "" {
				name = a.Email
			}
			if a.ResponseStatus != "" {
				name += " (" + a.ResponseStatus + ")"
			}
			names = append(names, name)
		}
		sb.WriteString(fmt.Sprintf("    Участники: %s\n", strings.Join(names, ", ")))
	}
	if len(e.Recurrence) > 0 {
		sb.WriteString(fmt.Sprintf("    Повтор: %s\n", strings.Join(e.Recurrence, ", ")))
	}
	sb.WriteString(fmt.Sprintf("    ID: %s", e.ID))
	if e.HTMLLink != "" {
		sb.WriteString(fmt.Sprintf(" · %s", e.HTMLLink))
	}
	sb.WriteString("\n")
	return sb.String()
}

// formatEventList renders a slice of events as compact text.
func formatEventList(events []calendarEvent, calendarID string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📅 События: %d (calendar: %s)\n\n", len(events), calendarID))
	for i, e := range events {
		sb.WriteString(formatEvent(e, i+1))
	}
	return sb.String()
}

// handleCalendarList implements the calendar_list MCP tool.
func handleCalendarList(args json.RawMessage) ToolResult {
	var p struct {
		TimeMin    string `json:"time_min"`
		TimeMax    string `json:"time_max"`
		MaxResults int    `json:"max_results"`
		CalendarID string `json:"calendar_id"`
		Account    string `json:"account"`
	}
	json.Unmarshal(args, &p)

	if p.CalendarID == "" {
		p.CalendarID = calendarDefaultCalendarID
	}
	if p.Account == "" {
		p.Account = googleDefaultAccount("read")
	}
	tz := calendarDefaultTimeZone()

	now := time.Now()
	timeMin := now
	if p.TimeMin != "" {
		t, err := parseRFC3339(p.TimeMin, "time_min")
		if err != nil {
			return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: err.Error()}}}
		}
		timeMin = t
	}
	timeMax := now.AddDate(0, 0, 7)
	if p.TimeMax != "" {
		t, err := parseRFC3339(p.TimeMax, "time_max")
		if err != nil {
			return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: err.Error()}}}
		}
		timeMax = t
	}
	if !timeMin.Before(timeMax) {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "time_min must be before time_max"}}}
	}

	events, err := calendarListEvents(p.Account, p.CalendarID, tz, timeMin, timeMax, p.MaxResults)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: formatEventList(events, p.CalendarID)}}}
}

// handleCalendarGet implements the calendar_get MCP tool.
func handleCalendarGet(args json.RawMessage) ToolResult {
	var p struct {
		EventID    string `json:"event_id"`
		CalendarID string `json:"calendar_id"`
		Account    string `json:"account"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Invalid arguments: " + err.Error()}}}
	}
	if p.EventID == "" {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "event_id is required"}}}
	}
	if p.CalendarID == "" {
		p.CalendarID = calendarDefaultCalendarID
	}
	if p.Account == "" {
		p.Account = googleDefaultAccount("read")
	}

	e, err := calendarGetEvent(p.Account, p.CalendarID, p.EventID)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: formatEvent(e, 0)}}}
}

// handleCalendarCreate implements the calendar_create MCP tool.
func handleCalendarCreate(args json.RawMessage) ToolResult {
	var p struct {
		Summary     string   `json:"summary"`
		Start       string   `json:"start"`
		End         string   `json:"end"`
		Description string   `json:"description"`
		Location    string   `json:"location"`
		Attendees   []string `json:"attendees"`
		TimeZone    string   `json:"time_zone"`
		CalendarID  string   `json:"calendar_id"`
		Account     string   `json:"account"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Invalid arguments: " + err.Error()}}}
	}
	if p.Summary == "" {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "summary is required"}}}
	}
	startTime, err := parseRFC3339(p.Start, "start")
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: err.Error()}}}
	}
	endTime, err := parseRFC3339(p.End, "end")
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: err.Error()}}}
	}
	if !startTime.Before(endTime) {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "start must be before end"}}}
	}
	if p.CalendarID == "" {
		p.CalendarID = calendarDefaultCalendarID
	}
	if p.Account == "" {
		p.Account = googleDefaultAccount("read")
	}
	if p.TimeZone == "" {
		p.TimeZone = calendarDefaultTimeZone()
	}

	var e calendarEvent
	e.Summary = p.Summary
	e.Description = p.Description
	e.Location = p.Location
	e.Start.DateTime = startTime.Format(time.RFC3339)
	e.Start.TimeZone = p.TimeZone
	e.End.DateTime = endTime.Format(time.RFC3339)
	e.End.TimeZone = p.TimeZone
	if len(p.Attendees) > 0 {
		for _, a := range attendeeEmails(p.Attendees) {
			e.Attendees = append(e.Attendees, struct {
				Email          string `json:"email,omitempty"`
				DisplayName    string `json:"displayName,omitempty"`
				ResponseStatus string `json:"responseStatus,omitempty"`
			}{Email: a["email"]})
		}
	}

	created, err := calendarCreateEvent(p.Account, p.CalendarID, e)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("✅ Создано: %s\n%s", created.ID, formatEvent(created, 0))}}}
}

// handleCalendarUpdate implements the calendar_update MCP tool (true PATCH).
func handleCalendarUpdate(args json.RawMessage) ToolResult {
	var p struct {
		EventID     string   `json:"event_id"`
		Summary     string   `json:"summary"`
		Start       string   `json:"start"`
		End         string   `json:"end"`
		Description string   `json:"description"`
		Location    string   `json:"location"`
		Attendees   []string `json:"attendees"`
		TimeZone    string   `json:"time_zone"`
		CalendarID  string   `json:"calendar_id"`
		Account     string   `json:"account"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Invalid arguments: " + err.Error()}}}
	}
	if p.EventID == "" {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "event_id is required"}}}
	}
	if p.CalendarID == "" {
		p.CalendarID = calendarDefaultCalendarID
	}
	if p.Account == "" {
		p.Account = googleDefaultAccount("read")
	}
	if p.TimeZone == "" {
		p.TimeZone = calendarDefaultTimeZone()
	}

	patch := map[string]any{}
	if p.Summary != "" {
		patch["summary"] = p.Summary
	}
	if p.Description != "" {
		patch["description"] = p.Description
	}
	if p.Location != "" {
		patch["location"] = p.Location
	}
	if p.Start != "" {
		t, err := parseRFC3339(p.Start, "start")
		if err != nil {
			return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: err.Error()}}}
		}
		patch["start"] = map[string]string{"dateTime": t.Format(time.RFC3339), "timeZone": p.TimeZone}
	}
	if p.End != "" {
		t, err := parseRFC3339(p.End, "end")
		if err != nil {
			return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: err.Error()}}}
		}
		patch["end"] = map[string]string{"dateTime": t.Format(time.RFC3339), "timeZone": p.TimeZone}
	}
	if p.Attendees != nil {
		patch["attendees"] = attendeeEmails(p.Attendees)
	}

	if len(patch) == 0 {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "no fields to update"}}}
	}

	updated, err := calendarPatchEvent(p.Account, p.CalendarID, p.EventID, patch)
	if err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("✅ Обновлено: %s\n%s", updated.ID, formatEvent(updated, 0))}}}
}

// handleCalendarDelete implements the calendar_delete MCP tool.
func handleCalendarDelete(args json.RawMessage) ToolResult {
	var p struct {
		EventID    string `json:"event_id"`
		CalendarID string `json:"calendar_id"`
		Account    string `json:"account"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "Invalid arguments: " + err.Error()}}}
	}
	if p.EventID == "" {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: "event_id is required"}}}
	}
	if p.CalendarID == "" {
		p.CalendarID = calendarDefaultCalendarID
	}
	if p.Account == "" {
		p.Account = googleDefaultAccount("read")
	}

	if err := calendarDeleteEvent(p.Account, p.CalendarID, p.EventID); err != nil {
		return ToolResult{IsError: true, Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("✅ Удалено событие %s", p.EventID)}}}
}
