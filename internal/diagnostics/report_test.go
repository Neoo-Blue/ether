package diagnostics

import (
	"encoding/json"
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
	body, _ := eventsJSONL(events, [3]bool{true, false, false}, "test", nil)
	rows, findings := analyzeJournal(body)
	if len(findings) != 2 || findings[0].Code != "duplicate_hub_across_directories" || len(findings[0].Lines) != 2 || findings[0].Lines[0] != 1 || findings[0].Lines[1] != 2 {
		t.Fatalf("findings: %+v", findings)
	}
	report := string(reportMarkdown(rows, findings, nil))
	if !strings.Contains(report, "Example") || !strings.Contains(report, "journal lines [1 2]") {
		t.Fatalf("report: %s", report)
	}
	body, _ = eventsJSONL(events, [3]bool{}, "test", nil)
	rows, findings = analyzeJournal(body)
	report = string(reportMarkdown(rows, findings, nil))
	if strings.Contains(report, "Example") {
		t.Fatal("title leaked with titles disabled")
	}
	if _, err := json.Marshal(findings); err != nil {
		t.Fatal(err)
	}
}
