package diagnostics

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestReportReferencesRedactedJournalLines(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := []Event{
		{Time: now, Kind: KindRequest, Route: "/hubs/promoted", Query: "contentDirectoryID=1", Response: &ResponseSummary{Hubs: []HubSummary{{Identifier: "movie.recent", Style: "shelf", Size: 1, ItemCount: 1, Items: []string{"Example"}}}}},
		{Time: now, Kind: KindRequest, Route: "/hubs/promoted", Query: "contentDirectoryID=2", Response: &ResponseSummary{Hubs: []HubSummary{{Identifier: "movie.recent", Style: "shelf", Size: 1, ItemCount: 1}}}},
		{Time: now, Kind: KindRequest, Route: "/hubs/continueWatching", Response: &ResponseSummary{Hubs: []HubSummary{{Identifier: "continueWatching"}}}},
	}
	body, _, _ := eventsJSONL(events, [3]bool{true, false, false}, "test", nil)
	rows, findings := analyzeJournal(body)
	if len(findings) != 2 || findings[0].Code != "duplicate_hub_across_directories" || len(findings[0].Lines) != 2 || findings[0].Lines[0] != 1 || findings[0].Lines[1] != 2 {
		t.Fatalf("findings: %+v", findings)
	}
	report := string(reportMarkdown(rows, findings, nil))
	if !strings.Contains(report, "Example") || !strings.Contains(report, "requests #1–2") || !strings.Contains(report, "## What happened") || !strings.Contains(report, "## Request timeline") {
		t.Fatalf("report: %s", report)
	}
	body, _, _ = eventsJSONL(events, [3]bool{}, "test", nil)
	rows, findings = analyzeJournal(body)
	report = string(reportMarkdown(rows, findings, nil))
	if strings.Contains(report, "Example") {
		t.Fatal("title leaked with titles disabled")
	}
	if _, err := json.Marshal(findings); err != nil {
		t.Fatal(err)
	}
}

func TestJournalByteBudgetKeepsWholeRecentEvents(t *testing.T) {
	var events []Event
	for i := 0; i < 100; i++ {
		events = append(events, Event{Time: time.Date(2026, 1, 1, 0, 0, 0, i, time.UTC), Kind: KindRequest, Route: fmt.Sprintf("/r/%d", i), Title: strings.Repeat("x", 12000)})
	}
	body, skipped, truncated := eventsJSONL(events, [3]bool{true, false, false}, "test", nil)
	if skipped != 0 || truncated == 0 || len(body) > 768<<10 || !strings.Contains(body, `"route":"/r/99"`) || strings.Contains(body, `"route":"/r/0"`) {
		t.Fatalf("skipped %d truncated %d bytes %d", skipped, truncated, len(body))
	}
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if !json.Valid([]byte(line)) {
			t.Fatal("journal cut a JSON line")
		}
	}
}
